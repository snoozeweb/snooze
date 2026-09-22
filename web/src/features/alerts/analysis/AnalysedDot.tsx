// A dot in the alerts table's severity cell: "someone has already worked out
// why this fired".
//
// It rides the severity cell rather than taking a column of its own — the fact
// is binary for most rows and a whole column of blanks would cost the message
// its width. The confidence tone is the only thing the dot encodes; the full
// sentence lives in `title`/`aria-label`, so the meaning survives both a
// screen reader and a colour-blind reader.
import type { Record_ } from "../types";
import { confidenceTone, isConfidence, type Confidence } from "./enums";
import styles from "./AnalysedDot.module.css";

export type AnalysedDotProps = {
  record: Record_;
};

/**
 * The analysis confidence stamped on a record, or undefined when the alert has
 * never been analysed.
 *
 * Read defensively: `agentic` is a protected subtree the generic Record schema
 * does not declare (records are dynamic, and the subtree is only written
 * through /record/{uid}/agentic), so it arrives as an unknown extra field.
 * Anything that is not one of the three known levels is treated as absent
 * rather than painted with a default tone.
 */
function recordConfidence(record: Record_): Confidence | undefined {
  const agentic = (record as { agentic?: unknown }).agentic;
  if (typeof agentic !== "object" || agentic === null) return undefined;
  const rootCause = (agentic as { root_cause?: unknown }).root_cause;
  if (typeof rootCause !== "object" || rootCause === null) return undefined;
  const confidence = (rootCause as { confidence?: unknown }).confidence;
  return isConfidence(confidence) ? confidence : undefined;
}

export function AnalysedDot({ record }: AnalysedDotProps) {
  const confidence = recordConfidence(record);
  if (confidence === undefined) return null;
  const label = `Analysed · ${confidence} confidence`;
  return (
    <span
      className={styles.dot}
      data-tone={confidenceTone(confidence)}
      role="img"
      title={label}
      aria-label={label}
    />
  );
}
