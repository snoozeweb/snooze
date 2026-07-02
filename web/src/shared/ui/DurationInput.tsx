// DurationInput — a text input with a leading badge that renders the value in
// human terms ("2d 4h", "forever", "—"). Accepts either human shorthand
// ("2h30m", "1d12h") or raw seconds ("9000"); both emit a number of seconds, so
// operators don't have to mentally convert big TTL / throttle / frequency values.
import { useEffect, useState } from "react";
import { secondsToHuman } from "@/lib/format/seconds";
import { parseDuration } from "@/lib/format/duration";
import styles from "./DurationInput.module.css";

export type DurationInputProps = {
  value: number | undefined;
  onChange: (next: number) => void;
  id?: string;
  "aria-label"?: string;
  placeholder?: string;
  /** Badge text when value === 0. Defaults to "forever" (TTL/throttle
   *  semantics). Pass "disabled" for fields where zero means the feature
   *  is turned off (frequency delay / repeat). */
  zeroLabel?: string;
};

// Turn user input into seconds: try the human-shorthand grammar first, then
// fall back to a bare integer count of seconds. Empty string means zero.
// Returns undefined for unparseable input so we don't emit a bogus value.
function parseInput(raw: string): number | undefined {
  const s = raw.trim();
  if (s === "") return 0;
  const short = parseDuration(s);
  if (short !== null) return short;
  if (/^\d+$/.test(s)) return Number(s);
  return undefined;
}

export function DurationInput({
  value,
  onChange,
  id,
  "aria-label": ariaLabel,
  placeholder,
  zeroLabel,
}: DurationInputProps) {
  // The input is free text; keep local state so a typed "2h30m" isn't snapped
  // back to "9000" mid-edit. Re-sync only when the external value changes to
  // something the current text doesn't already represent.
  const [text, setText] = useState<string>(value !== undefined ? String(value) : "");
  useEffect(() => {
    if (parseInput(text) !== value) {
      setText(value !== undefined ? String(value) : "");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- sync on external value only
  }, [value]);

  const badge = value === 0 && zeroLabel !== undefined ? zeroLabel : secondsToHuman(value);
  return (
    <div className={styles.wrap}>
      <span className={styles.badge} aria-hidden="true">
        {badge}
      </span>
      <input
        id={id}
        aria-label={ariaLabel}
        className={styles.input}
        type="text"
        inputMode="text"
        value={text}
        placeholder={placeholder ?? "e.g. 2h30m or 9000"}
        onChange={(e) => {
          setText(e.target.value);
          const next = parseInput(e.target.value);
          if (next !== undefined) onChange(next);
        }}
      />
    </div>
  );
}
