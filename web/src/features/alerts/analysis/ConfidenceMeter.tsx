// How much to trust a root cause, as a three-step meter plus the word.
//
// Deliberately NOT a severity-coloured chip. Confidence used to paint green /
// amber / red, which put a green pill beside a red Critical on every row —
// two opposite signals on one line — and made a low-confidence cause read as
// a second Critical. Trust is a quantity, not an alarm: the meter is ink on
// the surface, filled segments in --text-strong, empty ones in --border, so
// it reads at a glance from across a NOC room without borrowing a hue that
// already means something else in this product.
import { confidenceLabel, type Confidence } from "./enums";
import styles from "./ConfidenceMeter.module.css";

const FILLED: Record<Confidence, number> = { high: 3, medium: 2, low: 1 };

/** What each level claims, for the tooltip — the meter's legend. */
const MEANING: Record<Confidence, string> = {
  high: "High confidence: the cause is backed by direct evidence",
  medium: "Medium confidence: a likely cause, some links are inferred",
  low: "Low confidence: inconclusive — a trail to follow, not an answer",
};

export type ConfidenceMeterProps = {
  confidence: Confidence;
  /** `short` prints "High"; the default prints "High confidence". */
  wording?: "short" | "full";
  className?: string;
};

export function ConfidenceMeter({ confidence, wording = "full", className }: ConfidenceMeterProps) {
  const label = confidenceLabel(confidence);
  const filled = FILLED[confidence];
  return (
    <span
      className={[styles.meter, className].filter(Boolean).join(" ")}
      data-confidence={confidence}
      title={MEANING[confidence]}
    >
      <span className={styles.segments} aria-hidden="true">
        {[1, 2, 3].map((n) => (
          <span key={n} className={styles.segment} data-on={n <= filled ? "true" : undefined} />
        ))}
      </span>
      <span>
        {label}
        {wording === "full" ? " confidence" : <span className={styles.srOnly}> confidence</span>}
      </span>
    </span>
  );
}
