// TimeConstraintsEditor — bare-bones editor for the three families of
// time-window constraints (absolute datetime ranges, daily time-of-day
// windows, weekdays). Wire shape mirrors internal/timeconstraints/.
// Empty groups are treated as "always matches" by the backend, so the
// editor surfaces each family only when the user opts in by adding a
// constraint.
import { Button } from "@/shared/ui/Button";
import { IconButton } from "@/shared/ui/IconButton";
import { DateTimeRangePicker } from "@/shared/ui/DateTimeRangePicker";
import { WEEKDAY_LABELS, type TimeConstraintsGroup } from "@/lib/timeconstraints/types";
import styles from "./TimeConstraintsEditor.module.css";

export type TimeConstraintsEditorProps = {
  value: TimeConstraintsGroup | undefined;
  onChange: (next: TimeConstraintsGroup) => void;
};

export function TimeConstraintsEditor({ value, onChange }: TimeConstraintsEditorProps) {
  const g = value ?? {};
  const datetime = g.datetime ?? [];
  const time = g.time ?? [];
  const weekdays = g.weekdays?.[0]?.weekdays ?? [];

  // exactOptionalPropertyTypes: spread an empty object instead of
  // assigning `undefined`, so the resulting Group either has the key
  // present (with values) or omits it entirely.
  function setDatetime(next: typeof datetime) {
    const { datetime: _drop, ...rest } = g;
    onChange(next.length > 0 ? { ...rest, datetime: next } : rest);
  }
  function setTime(next: typeof time) {
    const { time: _drop, ...rest } = g;
    onChange(next.length > 0 ? { ...rest, time: next } : rest);
  }
  function setWeekdays(next: number[]) {
    const { weekdays: _drop, ...rest } = g;
    onChange(
      next.length > 0
        ? { ...rest, weekdays: [{ weekdays: [...next].sort((a, b) => a - b) }] }
        : rest,
    );
  }

  return (
    <div className={styles.stack}>
      {/* Weekdays — single OR group, click pills to toggle. */}
      <div>
        <div className={styles.subhead}>
          <span>Weekdays</span>
          {weekdays.length === 0 ? <span className={styles.empty}>(any)</span> : null}
        </div>
        <div className={styles.weekdayRow}>
          {WEEKDAY_LABELS.map((label, i) => {
            const active = weekdays.includes(i);
            return (
              <button
                key={label}
                type="button"
                className={styles.dayPill}
                data-active={active || undefined}
                aria-pressed={active}
                onClick={() => {
                  if (active) {
                    setWeekdays(weekdays.filter((d) => d !== i));
                  } else {
                    setWeekdays([...weekdays, i]);
                  }
                }}
              >
                {label}
              </button>
            );
          })}
        </div>
      </div>

      {/* Time-of-day windows — list of {from, until} pairs in HH:MM. */}
      <div>
        <div className={styles.subhead}>
          <span>Daily time windows</span>
          <Button
            size="sm"
            variant="ghost"
            leadingIcon="plus"
            onClick={() => setTime([...time, { from: "09:00", until: "17:00" }])}
          >
            Add window
          </Button>
        </div>
        {time.length === 0 ? <p className={styles.empty}>(all hours)</p> : null}
        {time.map((tc, idx) => (
          <div key={idx} className={styles.row}>
            <DateTimeRangePicker
              mode="time"
              // exactOptionalPropertyTypes: pass each side only when
              // defined, so the picker never sees a literal `undefined`
              // on the value object.
              value={{
                ...(tc.from !== undefined ? { from: tc.from } : {}),
                ...(tc.until !== undefined ? { until: tc.until } : {}),
              }}
              ariaLabelFrom={`Time window ${idx + 1} from`}
              ariaLabelUntil={`Time window ${idx + 1} until`}
              onChange={(next) => {
                const updated = [...time];
                updated[idx] = { ...tc, ...next };
                setTime(updated);
              }}
            />
            <IconButton
              icon="trash"
              variant="ghost"
              size="sm"
              label={`Remove time window ${idx + 1}`}
              onClick={() => setTime(time.filter((_, j) => j !== idx))}
            />
          </div>
        ))}
      </div>

      {/* Absolute datetime ranges — list of {from, until} ISO strings. */}
      <div>
        <div className={styles.subhead}>
          <span>Absolute date ranges</span>
          <Button
            size="sm"
            variant="ghost"
            leadingIcon="plus"
            onClick={() => setDatetime([...datetime, { from: "", until: "" }])}
          >
            Add range
          </Button>
        </div>
        {datetime.length === 0 ? <p className={styles.empty}>(open)</p> : null}
        {datetime.map((dc, idx) => (
          <div key={idx} className={styles.row}>
            <DateTimeRangePicker
              mode="datetime"
              value={{
                ...(dc.from !== undefined ? { from: dc.from } : {}),
                ...(dc.until !== undefined ? { until: dc.until } : {}),
              }}
              ariaLabelFrom={`Date range ${idx + 1} from`}
              ariaLabelUntil={`Date range ${idx + 1} until`}
              onChange={(next) => {
                const updated = [...datetime];
                updated[idx] = { ...dc, ...next };
                setDatetime(updated);
              }}
            />
            <IconButton
              icon="trash"
              variant="ghost"
              size="sm"
              label={`Remove date range ${idx + 1}`}
              onClick={() => setDatetime(datetime.filter((_, j) => j !== idx))}
            />
          </div>
        ))}
      </div>
    </div>
  );
}
