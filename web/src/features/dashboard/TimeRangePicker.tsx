import { DateTimeRangePicker } from "@/shared/ui/DateTimeRangePicker";
import type { StatsRange } from "./types";
import { isoToLocalInput, localInputToIso, presetToRange, type TimeRange } from "./time-range";
import styles from "./TimeRangePicker.module.css";

export type { TimeRange } from "./time-range";

export type TimeRangePickerProps = {
  value: TimeRange;
  onChange: (next: TimeRange) => void;
};

const PRESETS: ReadonlyArray<{ value: StatsRange; label: string }> = [
  { value: "1d", label: "1d" },
  { value: "1w", label: "1w" },
  { value: "1m", label: "1m" },
  { value: "1y", label: "1y" },
];

export function TimeRangePicker({ value, onChange }: TimeRangePickerProps) {
  function handlePreset(p: StatsRange) {
    const r = presetToRange(p);
    onChange({ range: p, ...r });
  }

  // The shared picker speaks "YYYY-MM-DDTHH:MM"; TimeRange holds UTC ISO.
  function handleCustom(next: { from?: string; until?: string }) {
    onChange({
      range: "custom",
      from: next.from ? localInputToIso(next.from) : "",
      to: next.until ? localInputToIso(next.until) : "",
    });
  }

  return (
    <div className={styles.bar}>
      {PRESETS.map((p) => (
        <button
          key={p.value}
          type="button"
          className={styles.preset}
          data-active={value.range === p.value}
          onClick={() => handlePreset(p.value)}
        >
          {p.label}
        </button>
      ))}
      <button
        type="button"
        className={styles.preset}
        data-active={value.range === "custom"}
        onClick={() => onChange({ ...value, range: "custom" })}
      >
        Custom
      </button>
      {value.range === "custom" ? (
        <div className={styles.custom}>
          <DateTimeRangePicker
            mode="datetime"
            value={{
              ...(value.from ? { from: isoToLocalInput(value.from) } : {}),
              ...(value.to ? { until: isoToLocalInput(value.to) } : {}),
            }}
            onChange={handleCustom}
            ariaLabelFrom="From"
            ariaLabelUntil="Until"
          />
        </div>
      ) : null}
    </div>
  );
}
