// A dot in the alerts table's severity cell: "someone has already worked out
// why this fired".
//
// It rides the severity cell rather than taking a column of its own — the fact
// is binary for most rows and a whole column of blanks would cost the message
// its width. The dot's tone is the plan's VERDICT, not the confidence: it sits
// one pixel from the severity badge, and a confidence painted green / amber /
// red there read as a second severity (a low-confidence cause was a red dot
// beside a red Critical). The verdict is a state the operator acts on — amber
// "act on it", green "recovered on its own", sage "somebody fixed it" (the
// closed-state hue), neutral otherwise — and the full
// sentence, confidence included, lives in `title`/`aria-label`, so the meaning
// survives both a screen reader and a colour-blind reader.
import type { Record_ } from "../types";
import { isConfidence, type Confidence } from "./enums";
import { isPlanStatus, planStatusLabel, type PlanStatus } from "./verdict";
import styles from "./AnalysedDot.module.css";

export type AnalysedDotProps = {
  record: Record_;
};

type DotTone = "ok" | "done" | "warning" | "neutral";

const TONES: Record<PlanStatus, DotTone> = {
  action_required: "warning",
  self_resolved: "ok",
  resolved: "done",
  monitoring: "neutral",
};

function asObject(value: unknown): Record<string, unknown> | undefined {
  return typeof value === "object" && value !== null
    ? (value as Record<string, unknown>)
    : undefined;
}

/**
 * The analysis confidence and plan verdict stamped on a record; `confidence`
 * is undefined when the alert has never been analysed.
 *
 * Read defensively: `agentic` is a protected subtree the generic Record schema
 * does not declare (records are dynamic, and the subtree is only written
 * through /record/{uid}/agentic), so it arrives as an unknown extra field.
 * Anything that is not one of the known values is treated as absent rather
 * than painted with a default tone.
 */
function recordAnalysis(record: Record_): {
  confidence: Confidence | undefined;
  status: PlanStatus | undefined;
} {
  const agentic = asObject((record as { agentic?: unknown }).agentic);
  const confidence = asObject(agentic?.["root_cause"])?.["confidence"];
  const status = asObject(agentic?.["remediation_plan"])?.["status"];
  return {
    confidence: isConfidence(confidence) ? confidence : undefined,
    status: isPlanStatus(status) ? status : undefined,
  };
}

export function AnalysedDot({ record }: AnalysedDotProps) {
  const { confidence, status } = recordAnalysis(record);
  if (confidence === undefined) return null;
  const label = [
    "Analysed",
    `${confidence} confidence`,
    ...(status !== undefined ? [planStatusLabel(status)] : []),
  ].join(" · ");
  return (
    <span
      className={styles.dot}
      data-tone={status !== undefined ? TONES[status] : "neutral"}
      role="img"
      title={label}
      aria-label={label}
    />
  );
}
