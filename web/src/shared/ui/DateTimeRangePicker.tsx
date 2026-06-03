// DateTimeRangePicker — chip-style trigger that opens a Radix Popover
// containing either:
//   - mode="time": two <input type="time"> spinners side by side, or
//   - mode="datetime": a react-day-picker (mode="range") + two
//                      <input type="time"> spinners.
//
// Wire shape is identical to the legacy native inputs:
//   - time mode emits "HH:MM" strings
//   - datetime mode emits "YYYY-MM-DDTHH:MM" strings (no seconds, no Z)
// so the Go backend sees the same payload it sees today.
//
// Restores the calendar+range visualisation that the old Vue UI offered
// via @vuepic/vue-datepicker, but stays inside the existing Radix +
// CSS-tokens design system.
import { useId, useMemo, useState } from "react";
import { DayPicker, type DateRange } from "react-day-picker";
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

// "YYYY-MM-DDTHH:MM" → { date: Date | null, time: "HH:MM" | "" }
//
// We pad to "HH:MM" so the underlying <input type="time"> always
// renders a value; an empty string would reset the spinner to its
// default placeholder which would be hard to control between renders.
function splitIsoLocal(s?: string): { date: Date | null; time: string } {
  if (!s) return { date: null, time: "" };
  // Accept the wire-shape "YYYY-MM-DDTHH:MM" *and* the full
  // RFC3339-with-seconds shape ("YYYY-MM-DDTHH:MM:SS...Z?") that the
  // backend may still hand us on initial load. We extract HH:MM only;
  // anything finer-grained gets dropped on the round-trip.
  const m = s.match(/^(\d{4})-(\d{2})-(\d{2})(?:T(\d{2}):(\d{2}))?/);
  if (!m) return { date: null, time: "" };
  const y = Number(m[1]);
  const mo = Number(m[2]);
  const d = Number(m[3]);
  const time = m[4] && m[5] ? `${m[4]}:${m[5]}` : "";
  // We construct the Date in local time on purpose — the wire format
  // has no timezone, so the calendar should display the same day the
  // user typed without UTC drift.
  const date = new Date(y, mo - 1, d);
  return { date, time };
}

// Compose a local Date + "HH:MM" back into the ISO-local wire format.
// We don't call Date.toISOString() because that would emit UTC; the
// backend wants the literal local-clock representation.
function composeIsoLocal(date: Date | null | undefined, time: string): string | undefined {
  if (!date) return undefined;
  const pad = (n: number) => String(n).padStart(2, "0");
  const y = date.getFullYear();
  const mo = pad(date.getMonth() + 1);
  const d = pad(date.getDate());
  const t = /^\d{2}:\d{2}$/.test(time) ? time : "00:00";
  return `${y}-${mo}-${d}T${t}`;
}

function formatDateOnly(s?: string): string {
  const { date } = splitIsoLocal(s);
  if (!date) return DATE_PLACEHOLDER;
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

function formatTime(s?: string): string {
  if (!s) return TIME_PLACEHOLDER;
  // Three input shapes need to be handled:
  //   "HH:MM"                         (time-mode wire shape)
  //   "YYYY-MM-DDTHH:MM"              (datetime-mode wire shape)
  //   "YYYY-MM-DDTHH:MM:SS(.fff)?(Z)?" (older RFC3339-with-seconds payloads
  //                                    that the backend may still emit on
  //                                    initial load; we just want HH:MM).
  const t = s.includes("T") ? s.split("T")[1] : s;
  const m = (t ?? "").match(/^(\d{2}:\d{2})/);
  return m?.[1] ?? TIME_PLACEHOLDER;
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
    const from = splitIsoLocal(value.from);
    const until = splitIsoLocal(value.until);
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
      return `${formatTime(value.from)} – ${formatTime(value.until)}`;
    }
    const fromLabel = value.from
      ? `${formatDateOnly(value.from)} ${formatTime(value.from)}`
      : `${DATE_PLACEHOLDER} ${TIME_PLACEHOLDER}`;
    const untilLabel = value.until
      ? `${formatDateOnly(value.until)} ${formatTime(value.until)}`
      : `${DATE_PLACEHOLDER} ${TIME_PLACEHOLDER}`;
    return `${fromLabel} → ${untilLabel}`;
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
  function emitDatetimeTime(side: "from" | "until", nextTime: string) {
    const fromIso = side === "from" ? composeIsoLocal(fromDate, nextTime) : value.from;
    const untilIso = side === "until" ? composeIsoLocal(untilDate, nextTime) : value.until;
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
    const newFrom = range.from ? composeIsoLocal(range.from, fromTime || "00:00") : undefined;
    const newUntil = range.to ? composeIsoLocal(range.to, untilTime || "23:59") : undefined;
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
