// DurationInput — a number input (seconds) with a leading badge that
// renders the value in human terms ("2d 4h", "forever", "—"). Mirrors the
// legacy Vue form/Duration.vue widget so users don't have to mentally
// convert big TTLs / throttle / frequency values.
import { secondsToHuman } from "@/lib/format/seconds";
import styles from "./DurationInput.module.css";

export type DurationInputProps = {
  value: number | undefined;
  onChange: (next: number) => void;
  id?: string;
  "aria-label"?: string;
  min?: number;
  step?: number;
  placeholder?: string;
};

export function DurationInput({
  value,
  onChange,
  id,
  "aria-label": ariaLabel,
  min,
  step,
  placeholder,
}: DurationInputProps) {
  return (
    <div className={styles.wrap}>
      <span className={styles.badge} aria-hidden="true">
        {secondsToHuman(value)}
      </span>
      <input
        id={id}
        aria-label={ariaLabel}
        className={styles.input}
        type="number"
        inputMode="numeric"
        value={value ?? ""}
        min={min}
        step={step}
        placeholder={placeholder}
        onChange={(e) => {
          const raw = e.target.value;
          if (raw === "") {
            onChange(0);
            return;
          }
          const n = Number(raw);
          if (Number.isFinite(n)) onChange(n);
        }}
      />
    </div>
  );
}
