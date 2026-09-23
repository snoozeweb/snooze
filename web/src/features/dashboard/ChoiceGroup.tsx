// A single-select row of chips: the Analyses view's filters and its sort.
//
// A radio group, with the whole contract one implies — `radiogroup` +
// `aria-checked`, one tab stop (the checked option), arrow keys that move the
// selection and wrap. Every group here is single-select, and the chips used to
// be a row of `aria-pressed` toggles that looked identical to the multi-select
// beside them while behaving differently; the role now says which kind of
// control it is, and so does the picture.
//
// The picture: the chosen chip is outlined in the accent with a check and
// stronger ink; the rest are quiet. Never a solid fill — when every chip of a
// multi-select started filled amber, "on" and "off" were the same picture and
// the state could not be read at all.
import { useId, useRef, type KeyboardEvent } from "react";
import { Icon } from "@/shared/icons/Icon";
import styles from "./ChoiceGroup.module.css";

export type Choice<T extends string> = {
  id: T;
  label: string;
  /** What the option means, when the label alone is terse ("Medium+"). */
  hint?: string;
};

export type ChoiceGroupProps<T extends string> = {
  /** The question the group answers. Visible, and the group's accessible name. */
  label: string;
  options: readonly Choice<T>[];
  value: T;
  onChange: (next: T) => void;
};

const NEXT_KEYS = new Set(["ArrowRight", "ArrowDown"]);
const PREV_KEYS = new Set(["ArrowLeft", "ArrowUp"]);

export function ChoiceGroup<T extends string>({
  label,
  options,
  value,
  onChange,
}: ChoiceGroupProps<T>) {
  const labelId = useId();
  const refs = useRef<(HTMLButtonElement | null)[]>([]);

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    const step = NEXT_KEYS.has(e.key) ? 1 : PREV_KEYS.has(e.key) ? -1 : 0;
    if (step === 0) return;
    e.preventDefault();
    const current = options.findIndex((o) => o.id === value);
    const next = (current + step + options.length) % options.length;
    const option = options[next];
    if (!option) return;
    onChange(option.id);
    refs.current[next]?.focus();
  }

  return (
    <div className={styles.group}>
      <span id={labelId} className={styles.label}>
        {label}
      </span>
      {/* The keys belong to the group, not to each radio: the pattern moves
          focus between the options, so one handler over all of them is the
          honest place for it. */}
      {/* eslint-disable-next-line jsx-a11y/interactive-supports-focus -- focus lives on the checked radio inside (roving tabindex); the group itself is not a tab stop by design. */}
      <div
        className={styles.options}
        role="radiogroup"
        aria-labelledby={labelId}
        onKeyDown={onKeyDown}
      >
        {options.map((o, i) => {
          const checked = o.id === value;
          return (
            <button
              key={o.id}
              ref={(el) => {
                refs.current[i] = el;
              }}
              type="button"
              role="radio"
              aria-checked={checked}
              tabIndex={checked ? 0 : -1}
              className={styles.option}
              title={o.hint}
              onClick={() => onChange(o.id)}
            >
              {checked ? <Icon name="check" size={12} /> : null}
              {o.label}
            </button>
          );
        })}
      </div>
    </div>
  );
}
