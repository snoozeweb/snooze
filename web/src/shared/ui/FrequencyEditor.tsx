// FrequencyEditor — three integer fields shaping notification rate-limit.
//   - total: max deliveries (0 / undefined = unlimited)
//   - delay: seconds before the first delivery
//   - every: seconds between subsequent deliveries
// Backend shape: internal/pluginimpl/notification.Frequency.
import type { Frequency } from "@/features/notifications/types";
import { Input } from "@/shared/ui/Input";
import { DurationInput } from "./DurationInput";
import styles from "./TimeConstraintsEditor.module.css";

export type FrequencyEditorProps = {
  value: Frequency | undefined;
  onChange: (next: Frequency) => void;
};

export function FrequencyEditor({ value, onChange }: FrequencyEditorProps) {
  const f = value ?? {};
  return (
    <div className={styles.stack}>
      <div>
        <div className={styles.subhead}>
          <span>Total (max deliveries; 0 = unlimited)</span>
        </div>
        <Input
          type="number"
          inputMode="numeric"
          value={String(f.total ?? "")}
          placeholder="0"
          onChange={(e) =>
            onChange({ ...f, total: e.target.value === "" ? 0 : Number(e.target.value) })
          }
          aria-label="Frequency total"
        />
      </div>
      <div>
        <div className={styles.subhead}>
          <span>Initial delay (0 = disabled)</span>
        </div>
        <DurationInput
          aria-label="Frequency delay"
          value={f.delay ?? 0}
          onChange={(n) => onChange({ ...f, delay: n })}
          zeroLabel="disabled"
        />
      </div>
      <div>
        <div className={styles.subhead}>
          <span>Repeat every (0 = disabled)</span>
        </div>
        <DurationInput
          aria-label="Frequency every"
          value={f.every ?? 0}
          onChange={(n) => onChange({ ...f, every: n })}
          zeroLabel="disabled"
        />
      </div>
    </div>
  );
}

