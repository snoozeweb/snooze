// "412 / 500" under a length-limited field.
//
// It counts CODE POINTS, not UTF-16 units, because that is what the server
// counts (`utf8.RuneCountInString`) — a counter that disagreed with the
// validator would read "487 / 500" next to a rejected save.
//
// It subscribes to its own field through `useWatch`, so a keystroke in the
// summary re-renders the summary's counter and nothing else.
import { useWatch, type Control, type FieldPath } from "react-hook-form";
import { runeLength, type AnalysisForm } from "./schema";
import styles from "./CharCounter.module.css";

export type CharCounterProps = {
  control: Control<AnalysisForm>;
  name: FieldPath<AnalysisForm>;
  limit: number;
  /** Wire the counter into the field's `aria-describedby`. */
  id?: string;
};

export function CharCounter({ control, name, limit, id }: CharCounterProps) {
  const value = useWatch({ control, name }) as unknown;
  const used = runeLength(typeof value === "string" ? value : "");
  const over = used > limit;
  return (
    <span
      {...(id !== undefined ? { id } : {})}
      className={[styles.counter, over ? styles.over : null].filter(Boolean).join(" ")}
    >
      {`${used} / ${limit}`}
    </span>
  );
}
