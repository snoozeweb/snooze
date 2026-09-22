// The observations a root cause rests on, as an editable ordered list.
//
// Order carries meaning here — EvidenceList renders the items "in the order
// the analysis recorded them" — so the list gets explicit Move up / Move down
// buttons rather than only drag (which the step lists also offer): evidence is
// a handful of short lines, and two buttons beat mounting a second dnd-kit
// context for them.
//
// The inputs are CONTROLLED (value + setValue) rather than `register`ed. A
// registered input keeps its value on the DOM node, and removing item 2 of 4
// would leave three nodes holding the old values while react-hook-form holds
// the new ones; a controlled row re-renders from the single source of truth.
import { useEffect, useId, useRef } from "react";
import { useWatch, type Control, type UseFormSetValue } from "react-hook-form";
import type { FieldErrors } from "react-hook-form";
import { Button } from "@/shared/ui/Button";
import { IconButton } from "@/shared/ui/IconButton";
import { Input } from "@/shared/ui/Input";
import { describeFieldError, errorAt } from "./fieldErrors";
import { ANALYSIS_LIMITS, runeLength, type AnalysisForm } from "./schema";
import styles from "./editor.module.css";
import rowStyles from "./EvidenceListEditor.module.css";

const FIELD = "root_cause.evidence";

export type EvidenceListEditorProps = {
  control: Control<AnalysisForm>;
  setValue: UseFormSetValue<AnalysisForm>;
  errors: FieldErrors<AnalysisForm>;
  /** Re-run the resolver on every edit once the author has tried to save. */
  validateOnChange: boolean;
};

export function EvidenceListEditor({
  control,
  setValue,
  errors,
  validateOnChange,
}: EvidenceListEditorProps) {
  const baseId = useId();
  const items = useWatch({ control, name: FIELD }) ?? [];
  const atCap = items.length >= ANALYSIS_LIMITS.evidenceItems;
  const addId = `${baseId}-add`;
  // The id of the control focus should land on once the list has re-rendered.
  // Both edits move it: an Add whose new box is not focused makes a keyboard
  // user hunt for what they just asked for, and a Remove that leaves focus on
  // a button that no longer exists drops it to <body>, which puts the next Tab
  // back at the top of the page.
  const focusTarget = useRef<string | null>(null);

  useEffect(() => {
    const id = focusTarget.current;
    if (id === null) return;
    focusTarget.current = null;
    document.getElementById(id)?.focus();
  }, [baseId, items.length]);

  function commit(next: string[]) {
    setValue(FIELD, next, { shouldDirty: true, shouldValidate: validateOnChange });
  }

  function add() {
    focusTarget.current = `${baseId}-item-${items.length}`;
    commit([...items, ""]);
  }

  function removeAt(index: number) {
    const next = items.filter((_, k) => k !== index);
    // The row that slid into the removed one's place, or the row above when
    // the last one went; the Add button when nothing is left to focus.
    focusTarget.current =
      next.length === 0 ? addId : `${baseId}-item-${Math.min(index, next.length - 1)}`;
    commit(next);
  }

  function swap(from: number, to: number) {
    if (to < 0 || to >= items.length) return;
    const next = items.slice();
    const a = next[from];
    const b = next[to];
    if (a === undefined || b === undefined) return;
    next[from] = b;
    next[to] = a;
    commit(next);
  }

  const listError = describeFieldError("The evidence list", errorAt(errors, FIELD));

  return (
    <div className={styles.field}>
      <div className={styles.labelRow}>
        <span className={styles.label} id={`${baseId}-label`}>
          Evidence
        </span>
        <span className={styles.hint}>{`${items.length} / ${ANALYSIS_LIMITS.evidenceItems}`}</span>
      </div>
      <p className={styles.hint} id={`${baseId}-hint`}>
        One observation per line — a probe and what it showed, in the order they were gathered.
      </p>
      {items.length === 0 ? (
        <p className={styles.hint}>No evidence recorded. A conclusion with no trail is a guess.</p>
      ) : (
        <ol className={rowStyles.list} aria-labelledby={`${baseId}-label`}>
          {items.map((item, i) => {
            const itemId = `${baseId}-item-${i}`;
            const message = describeFieldError(
              `Evidence ${i + 1}`,
              errorAt(errors, `${FIELD}[${i}]`),
            );
            return (
              // Index keys: the rows ARE their positions here (moving item 2
              // up is meant to move the box, not carry a React identity with
              // it), and the values are free text with no stable id.
              <li key={i} className={rowStyles.item}>
                <div className={rowStyles.inputCell}>
                  <Input
                    id={itemId}
                    aria-label={`Evidence ${i + 1}`}
                    aria-describedby={`${baseId}-hint`}
                    value={item}
                    invalid={message !== undefined}
                    errorMessage={message}
                    onChange={(e) => commit(items.map((v, k) => (k === i ? e.target.value : v)))}
                  />
                </div>
                <span className={rowStyles.count}>
                  {`${runeLength(item)} / ${ANALYSIS_LIMITS.evidence}`}
                </span>
                <div className={styles.row}>
                  <IconButton
                    icon="chevron-up"
                    label={`Move evidence ${i + 1} up`}
                    size="sm"
                    disabled={i === 0}
                    onClick={() => swap(i, i - 1)}
                  />
                  <IconButton
                    icon="chevron-down"
                    label={`Move evidence ${i + 1} down`}
                    size="sm"
                    disabled={i === items.length - 1}
                    onClick={() => swap(i, i + 1)}
                  />
                  <IconButton
                    icon="trash"
                    label={`Remove evidence ${i + 1}`}
                    variant="ghostDanger"
                    size="sm"
                    onClick={() => removeAt(i)}
                  />
                </div>
              </li>
            );
          })}
        </ol>
      )}
      {listError !== undefined ? (
        <p className={styles.error} role="alert">
          {listError}
        </p>
      ) : null}
      <Button
        id={addId}
        size="sm"
        variant="secondary"
        leadingIcon="plus"
        className={styles.addButton}
        disabled={atCap}
        {...(atCap
          ? { title: `An analysis holds at most ${ANALYSIS_LIMITS.evidenceItems} items` }
          : {})}
        onClick={add}
      >
        Add evidence
      </Button>
      {atCap ? (
        <p className={styles.hint}>
          {`That is the limit of ${ANALYSIS_LIMITS.evidenceItems} items. Remove one to add another.`}
        </p>
      ) : null}
    </div>
  );
}
