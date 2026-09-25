// A multi-select row of chips: the Analyses view's Verdict filter.
//
// ChoiceGroup's sibling, and deliberately a different control: a `group` of
// `checkbox`es, not a `radiogroup`. The picture is the same vocabulary — a
// ticked chip is outlined in the accent with a check, an unticked one is quiet,
// never a solid fill — so "on" and "off" read the same way in both kinds, and
// the role says which kind it is.
//
// Keyboard: one tab stop for the options (roving tabindex), the arrow keys
// move focus between them WITHOUT changing anything, and Space / Enter toggle
// the focused chip (a button's own activation). Moving the selection with the
// arrows, as a radio group does, would untick and tick chips on the way past.
//
// `presets` are one-click selections ("All") rendered after the options; one
// that would change nothing is omitted rather than shown dead.
import { useId, useRef, useState, type KeyboardEvent } from "react";
import { Icon } from "@/shared/icons/Icon";
import type { Choice } from "./ChoiceGroup";
import styles from "./ChoiceGroup.module.css";

export type MultiChoicePreset<T extends string> = {
  label: string;
  value: readonly T[];
  hint?: string;
};

export type MultiChoiceGroupProps<T extends string> = {
  /** The question the group answers. Visible, and the group's accessible name. */
  label: string;
  options: readonly Choice<T>[];
  value: readonly T[];
  /** The next selection, in `options` order. */
  onChange: (next: T[]) => void;
  presets?: readonly MultiChoicePreset<T>[];
};

const NEXT_KEYS = new Set(["ArrowRight", "ArrowDown"]);
const PREV_KEYS = new Set(["ArrowLeft", "ArrowUp"]);

function sameSet<T>(a: readonly T[], b: readonly T[]): boolean {
  return a.length === b.length && a.every((x) => b.includes(x));
}

export function MultiChoiceGroup<T extends string>({
  label,
  options,
  value,
  onChange,
  presets = [],
}: MultiChoiceGroupProps<T>) {
  const labelId = useId();
  const refs = useRef<(HTMLButtonElement | null)[]>([]);
  // The option holding the group's one tab stop; the first by default.
  const [focusIndex, setFocusIndex] = useState(0);

  function toggle(id: T) {
    const next = value.includes(id) ? value.filter((v) => v !== id) : [...value, id];
    onChange(options.map((o) => o.id).filter((o) => next.includes(o)));
  }

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    const step = NEXT_KEYS.has(e.key) ? 1 : PREV_KEYS.has(e.key) ? -1 : 0;
    if (step === 0) return;
    e.preventDefault();
    const next = (focusIndex + step + options.length) % options.length;
    setFocusIndex(next);
    refs.current[next]?.focus();
  }

  const shownPresets = presets.filter((p) => !sameSet(p.value, value));

  return (
    <div className={styles.group}>
      <span id={labelId} className={styles.label}>
        {label}
      </span>
      {/* The arrow keys belong to the group: they move focus BETWEEN its
          checkboxes, so the one handler that sees all of them is the honest
          place for it. The group is not a tab stop; one checkbox is. */}
      {/* eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions -- delegated roving focus; each checkbox is the focus target. */}
      <div className={styles.options} role="group" aria-labelledby={labelId} onKeyDown={onKeyDown}>
        {options.map((o, i) => {
          const checked = value.includes(o.id);
          return (
            <button
              key={o.id}
              ref={(el) => {
                refs.current[i] = el;
              }}
              type="button"
              role="checkbox"
              aria-checked={checked}
              tabIndex={i === focusIndex ? 0 : -1}
              className={styles.option}
              title={o.hint}
              onFocus={() => setFocusIndex(i)}
              onClick={() => toggle(o.id)}
            >
              {checked ? <Icon name="check" size={12} /> : null}
              {o.label}
            </button>
          );
        })}
        {shownPresets.map((p) => (
          <button
            key={p.label}
            type="button"
            className={styles.preset}
            title={p.hint}
            onClick={() => onChange(options.map((o) => o.id).filter((o) => p.value.includes(o)))}
          >
            {p.label}
          </button>
        ))}
      </div>
    </div>
  );
}
