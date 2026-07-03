// DateTimeRangePicker — chip-style trigger that opens a Radix Popover
// containing either:
//   - mode="time": two <input type="time"> spinners side by side, or
//   - mode="datetime": a react-day-picker (mode="range") + two
//                      <input type="time"> spinners.
//
// Wire shape:
//   - time mode emits "HH:MM" strings (a recurring wall clock — unchanged)
//   - datetime mode emits ZONED RFC3339 ("YYYY-MM-DDTHH:MM:SS±HH:MM"), with the
//     offset computed for the picked date, so the backend stores the exact
//     instant the operator meant instead of parsing a zone-less value as UTC.
//     See lib/timeconstraints/datetimeWire.ts for the round-trip rationale
//     (incl. why this is DST-safe and how legacy zone-less values are read).
//
// Restores the calendar+range visualisation that the old Vue UI offered
// via @vuepic/vue-datepicker, but stays inside the existing Radix +
// CSS-tokens design system.
import { useId, useMemo, useState } from "react";
import { DayPicker, type DateRange } from "react-day-picker";
import { composeZonedIso, splitZonedIso } from "@/lib/timeconstraints/datetimeWire";
import { Popover, PopoverContent, PopoverTrigger } from "./Popover";
import styles from "./DateTimeRangePicker.module.css";
// react-day-picker ships its own structural CSS that defines the layout
// of the calendar grid (table sizing, day cells, weekday header). The
// .module.css alongside this file *only* themes colours/borders on top
// of those classes.
import "react-day-picker/style.css";

export type DateTimeRangePickerMode = "time" | "datetime";

export type DateTimeRangePickerProps = {
  mode: DateTimeRangePickerMode;
  // mode=time:     { from?: "HH:MM",            until?: "HH:MM" }
  // mode=datetime: { from?: "YYYY-MM-DDTHH:MM", until?: "YYYY-MM-DDTHH:MM" }
  value: { from?: string; until?: string };
  onChange: (next: { from?: string; until?: string }) => void;
  ariaLabelFrom: string;
  ariaLabelUntil: string;
  disabled?: boolean;
};

const TIME_PLACEHOLDER = "--:--";
const DATE_PLACEHOLDER = "----/--/--";

// Module-scope classNames map for react-day-picker. CSS-module imports
// type each key as `string | undefined` under noUncheckedIndexedAccess;
// the `??` chains collapse them to plain strings so the prop satisfies
// react-day-picker's `Partial<ClassNames>` (which forbids `undefined`
// values under exactOptionalPropertyTypes).
const dpClassNames = {
  root: styles.dpRoot ?? "",
  day: styles.dpDay ?? "",
  day_button: styles.dpDayButton ?? "",
  selected: styles.dpSelected ?? "",
  range_start: styles.dpRangeStart ?? "",
  range_middle: styles.dpRangeMiddle ?? "",
  range_end: styles.dpRangeEnd ?? "",
  today: styles.dpToday ?? "",
  outside: styles.dpOutside ?? "",
  disabled: styles.dpDisabled ?? "",
  month_caption: styles.dpCaption ?? "",
  caption_label: styles.dpCaptionLabel ?? "",
  weekdays: styles.dpWeekdays ?? "",
  weekday: styles.dpWeekday ?? "",
  nav: styles.dpNav ?? "",
  button_previous: styles.dpNavButton ?? "",
  button_next: styles.dpNavButton ?? "",
};

const pad = (n: number): string => String(n).padStart(2, "0");

// formatTimeOfDay renders a time-mode value ("HH:MM", tolerating a legacy
// "HH:MM:SS±HH:MM") down to "HH:MM".
function formatTimeOfDay(s?: string): string {
  if (!s) return TIME_PLACEHOLDER;
  const m = s.match(/^(\d{2}):(\d{2})/);
  return m ? `${m[1]}:${m[2]}` : TIME_PLACEHOLDER;
}

// datetimeLabel renders a datetime-mode wire value as "YYYY-MM-DD HH:MM" in the
// viewer's local zone (zoned values) or its literal wall clock (legacy
// zone-less values) — the split rules live in splitZonedIso.
function datetimeLabel(s?: string): string {
  const placeholder = `${DATE_PLACEHOLDER} ${TIME_PLACEHOLDER}`;
  if (!s) return placeholder;
  const { date, time } = splitZonedIso(s);
  if (!date) return placeholder;
  const dateStr = `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
  return `${dateStr} ${time || TIME_PLACEHOLDER}`;
}

export function DateTimeRangePicker({
  mode,
  value,
  onChange,
  ariaLabelFrom,
  ariaLabelUntil,
  disabled,
}: DateTimeRangePickerProps) {
  const [open, setOpen] = useState(false);
  const triggerId = useId();

  // Pre-compute the calendar's selected range (datetime mode only).
  const { fromDate, fromTime, untilDate, untilTime, calendarSelected } = useMemo(() => {
    const from = splitZonedIso(value.from);
    const until = splitZonedIso(value.until);
    let selected: DateRange | undefined;
    if (from.date || until.date) {
      // react-day-picker's DateRange wants `from` set when there's a
      // single end of the range. If only `until` exists, we still pass
      // `from: until` so the calendar shows *something* highlighted.
      selected = {
        from: from.date ?? until.date ?? undefined,
        to: until.date ?? undefined,
      };
    }
    return {
      fromDate: from.date,
      fromTime: from.time,
      untilDate: until.date,
      untilTime: until.time,
      calendarSelected: selected,
    };
  }, [value.from, value.until]);

  const triggerLabel = useMemo(() => {
    if (mode === "time") {
      return `${formatTimeOfDay(value.from)} – ${formatTimeOfDay(value.until)}`;
    }
    return `${datetimeLabel(value.from)} → ${datetimeLabel(value.until)}`;
  }, [mode, value.from, value.until]);

  const triggerAriaLabel = `${ariaLabelFrom} / ${ariaLabelUntil} (${triggerLabel})`;

  // ── time mode change handlers ─────────────────────────────────────────
  // Empty-string => emit undefined for that side so the parent's
  // setTime/setDatetime helpers can prune the constraint entirely.
  // We spread the existing-from/until conditionally so that, under
  // exactOptionalPropertyTypes, we never pass a literal `undefined` —
  // the key is either present with a string or absent.
  function emitTime(side: "from" | "until", next: string) {
    const cleaned = next === "" ? undefined : next;
    const nextFrom = side === "from" ? cleaned : value.from;
    const nextUntil = side === "until" ? cleaned : value.until;
    onChange({
      ...(nextFrom !== undefined ? { from: nextFrom } : {}),
      ...(nextUntil !== undefined ? { until: nextUntil } : {}),
    });
  }

  // ── datetime mode change handlers ─────────────────────────────────────
  // Recompose BOTH bounds from their current (date, time) split — not just the
  // edited one. Both already display in the viewer's local zone, so this
  // normalizes the whole range to a single zoned frame. Passing the untouched
  // bound through verbatim would instead leave a legacy zone-less value beside
  // a freshly zoned one; the backend then reads the zone-less side as UTC and
  // the zoned side as local, which can silently invert a narrow window into one
  // that never matches.
  function emitDatetimeTime(side: "from" | "until", nextTime: string) {
    const fromIso = composeZonedIso(fromDate, side === "from" ? nextTime : fromTime);
    const untilIso = composeZonedIso(untilDate, side === "until" ? nextTime : untilTime);
    onChange({
      ...(fromIso !== undefined ? { from: fromIso } : {}),
      ...(untilIso !== undefined ? { until: untilIso } : {}),
    });
  }

  function emitDatetimeRange(range: DateRange | undefined) {
    if (!range) {
      onChange({});
      return;
    }
    // Preserve the existing time-of-day when the user only picked dates;
    // default to 00:00 / 23:59 when there's no prior value.
    const newFrom = range.from ? composeZonedIso(range.from, fromTime || "00:00") : undefined;
    const newUntil = range.to ? composeZonedIso(range.to, untilTime || "23:59") : undefined;
    onChange({
      ...(newFrom !== undefined ? { from: newFrom } : {}),
      ...(newUntil !== undefined ? { until: newUntil } : {}),
    });
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          id={triggerId}
          type="button"
          className={styles.trigger}
          aria-label={triggerAriaLabel}
          aria-haspopup="dialog"
          disabled={disabled}
        >
          <span className={styles.triggerLabel}>{triggerLabel}</span>
        </button>
      </PopoverTrigger>
      <PopoverContent className={styles.popover ?? ""}>
        {mode === "datetime" ? (
          <div className={styles.calendarWrap}>
            <DayPicker
              mode="range"
              selected={calendarSelected}
              onSelect={emitDatetimeRange}
              numberOfMonths={1}
              showOutsideDays
              classNames={dpClassNames}
            />
          </div>
        ) : null}
        <div className={styles.timeRow}>
          <input
            className={styles.timeInput}
            type="time"
            value={mode === "time" ? (value.from ?? "") : fromTime}
            aria-label={ariaLabelFrom}
            onChange={(e) => {
              if (mode === "time") {
                emitTime("from", e.target.value);
              } else {
                emitDatetimeTime("from", e.target.value);
              }
            }}
          />
          <span aria-hidden="true" className={styles.timeSep}>
            –
          </span>
          <input
            className={styles.timeInput}
            type="time"
            value={mode === "time" ? (value.until ?? "") : untilTime}
            aria-label={ariaLabelUntil}
            onChange={(e) => {
              if (mode === "time") {
                emitTime("until", e.target.value);
              } else {
                emitDatetimeTime("until", e.target.value);
              }
            }}
          />
        </div>
      </PopoverContent>
    </Popover>
  );
}
