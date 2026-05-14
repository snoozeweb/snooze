import { Input } from "@/shared/ui/Input";
import type { StatsRange } from "./types";
import styles from "./TimeRangePicker.module.css";

export type TimeRange = {
  range: StatsRange;
  from: string;
  to: string;
};

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

export function presetToRange(
  range: StatsRange,
  now: Date = new Date(),
): { from: string; to: string } {
  if (range === "custom") return { from: "", to: "" };
  const ms: Record<Exclude<StatsRange, "custom">, number> = {
    "1d": 86_400_000,
    "1w": 7 * 86_400_000,
    "1m": 30 * 86_400_000,
    "1y": 365 * 86_400_000,
  };
  const to = now.toISOString();
  const from = new Date(now.getTime() - ms[range]).toISOString();
  return { from, to };
}

export function TimeRangePicker({ value, onChange }: TimeRangePickerProps) {
  function handlePreset(p: StatsRange) {
    const r = presetToRange(p);
    onChange({ range: p, ...r });
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
          <Input
            type="date"
            value={value.from.slice(0, 10)}
            onChange={(e) => onChange({ ...value, from: new Date(e.target.value).toISOString() })}
          />
          <span style={{ color: "var(--text-muted)" }}>—</span>
          <Input
            type="date"
            value={value.to.slice(0, 10)}
            onChange={(e) => onChange({ ...value, to: new Date(e.target.value).toISOString() })}
          />
        </div>
      ) : null}
    </div>
  );
}
