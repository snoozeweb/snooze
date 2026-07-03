// TimeConstraintsCell — renders a TimeConstraintsGroup as up to three
// rows (Weekdays / Hours / Dates) laid out as label | value pairs.
// The label column is right-aligned muted small text; the value column
// stacks one or more value lines.  When the group is fully empty (or
// undefined), renders the literal "Always" instead.
import type { TimeConstraintsGroup } from "@/lib/timeconstraints/types";
import { WEEKDAY_LABELS } from "@/lib/timeconstraints/types";
import styles from "./TimeConstraintsCell.module.css";

export type TimeConstraintsCellProps = {
  value: TimeConstraintsGroup | undefined;
};

export function TimeConstraintsCell({ value }: TimeConstraintsCellProps) {
  const wd = value?.weekdays?.[0]?.weekdays ?? [];
  const time = value?.time ?? [];
  const dates = value?.datetime ?? [];

  if (wd.length === 0 && time.length === 0 && dates.length === 0) {
    return <span className={styles.muted}>Always</span>;
  }

  // The recurring families (weekdays / hours) are read in this named zone; show
  // it so the times aren't a silent UTC default.
  const tz = value?.tz;
  const showTz = !!tz && (wd.length > 0 || time.length > 0);

  return (
    <span className={styles.wrap}>
      {wd.length > 0 ? (
        <span className={styles.block}>
          <span className={styles.label}>Weekdays</span>
          <span className={styles.values}>
            <span className={styles.line}>{formatWeekdays(wd)}</span>
          </span>
        </span>
      ) : null}
      {time.length > 0 ? (
        <span className={styles.block}>
          <span className={styles.label}>Hours</span>
          <span className={styles.values}>
            {time.map((t, i) => (
              <span key={i} className={styles.lineMono}>
                {formatHourRange(t.from, t.until)}
              </span>
            ))}
          </span>
        </span>
      ) : null}
      {showTz ? (
        <span className={styles.block}>
          <span className={styles.label}>Zone</span>
          <span className={styles.values}>
            <span className={styles.line}>{tz}</span>
          </span>
        </span>
      ) : null}
      {dates.length > 0 ? (
        <span className={styles.block}>
          <span className={styles.label}>Dates</span>
          <span className={styles.values}>
            {dates.map((d, i) => (
              <span key={i} className={styles.lineMono}>
                {formatDateRange(d.from, d.until)}
              </span>
            ))}
          </span>
        </span>
      ) : null}
    </span>
  );
}

function formatWeekdays(wd: number[]): string {
  if (wd.length === 7) return "Every day";
  const sorted = [...new Set(wd)].sort((a, b) => a - b);
  return sorted.map((i) => WEEKDAY_LABELS[i]).join(" · ");
}

// stripSeconds turns "HH:MM[:SS][±HH:MM]" into "HH:MM".
function stripSeconds(s: string): string {
  // Take the first 5 chars (HH:MM); the wire format always starts with that.
  return s.slice(0, 5);
}

function formatHourRange(from?: string, until?: string): string {
  if (from && until) return `${stripSeconds(from)} – ${stripSeconds(until)}`;
  if (from) return `from ${stripSeconds(from)}`;
  if (until) return `until ${stripSeconds(until)}`;
  return "any time";
}

// formatDateTime renders an RFC3339-ish string as "YYYY-MM-DD HH:mm" in
// local time. Falls back to the raw string if Date parsing fails.
//
// It uses LOCAL accessors (getFullYear/getHours/…), not getUTC*: a zone-less
// wire value like "2026-07-02T09:00" is parsed by Date as local time, so
// reading it back with getUTC* re-shifted it by the viewer's offset and showed
// a different clock than the operator authored. Local accessors render the same
// wall-clock for a zone-less value, and render an offset/Z value in the
// viewer's own zone — consistent either way.
function formatDateTime(s: string): string {
  const pad = (n: number) => n.toString().padStart(2, "0");
  // A bare calendar date ("YYYY-MM-DD") is parsed by `new Date` as UTC
  // midnight, which local accessors then shift across the date line in western
  // zones. It is already the display form, so return it verbatim.
  if (/^\d{4}-\d{2}-\d{2}$/.test(s)) return s;
  const d = new Date(s);
  if (Number.isNaN(d.getTime())) return s;
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function formatDateRange(from?: string, until?: string): string {
  if (from && until) return `${formatDateTime(from)} → ${formatDateTime(until)}`;
  if (from) return `from ${formatDateTime(from)}`;
  if (until) return `until ${formatDateTime(until)}`;
  return "any date";
}
