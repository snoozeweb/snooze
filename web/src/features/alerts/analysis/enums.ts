// The two closed enums of the agentic analysis, and how they render.
//
// `confidence` (how much to trust the conclusion) and `risk` (blast radius of
// a remediation step) are defined once, here, and nowhere else: every badge,
// filter chip, select and dot in the Analysis surfaces imports from this file.
// The orders below are the orders the server declares in
// `pkg/snoozetypes/agentic.go` and `api/openapi.yaml` — confidence descends
// (high first, the best case), risk ascends (low first, the safe case) — and
// they are also the orders the option lists render in, so a reader never has
// to re-learn a control's direction.
//
// Only risk takes a tone. Confidence used to paint green/amber/red too, which
// set a green pill beside a red Critical and made a low-confidence cause read
// as a second Critical; it now renders as a neutral meter (ConfidenceMeter).

/** Accepted values of `root_cause.confidence`, best first. */
export const CONFIDENCE_LEVELS = ["high", "medium", "low"] as const;

/** Accepted values of a step's `risk`, safest first. */
export const RISK_LEVELS = ["low", "medium", "high"] as const;

export type Confidence = (typeof CONFIDENCE_LEVELS)[number];
export type Risk = (typeof RISK_LEVELS)[number];

/**
 * The severity-token family a badge or dot paints with. Callers map it onto
 * `--severity-ok` / `--severity-warning` / `--severity-critical`; the accent
 * is never used here (it is reserved for interactive chrome).
 */
export type Tone = "ok" | "warning" | "critical";

const RISK_TONES: Record<Risk, Tone> = {
  low: "ok",
  medium: "warning",
  high: "critical",
};

const CONFIDENCE_LABELS: Record<Confidence, string> = {
  high: "High",
  medium: "Medium",
  low: "Low",
};

const RISK_LABELS: Record<Risk, string> = {
  low: "Low",
  medium: "Medium",
  high: "High",
};

/** isConfidence narrows an arbitrary string off the wire or out of a form. */
export function isConfidence(value: unknown): value is Confidence {
  return typeof value === "string" && (CONFIDENCE_LEVELS as readonly string[]).includes(value);
}

/** isRisk narrows an arbitrary string off the wire or out of a form. */
export function isRisk(value: unknown): value is Risk {
  return typeof value === "string" && (RISK_LEVELS as readonly string[]).includes(value);
}

/** Tone for a step's risk: low is reassuring, high is not. */
export function riskTone(level: Risk): Tone {
  return RISK_TONES[level];
}

/** Human label for a confidence level. Colour is never the only carrier. */
export function confidenceLabel(level: Confidence): string {
  return CONFIDENCE_LABELS[level];
}

/** Human label for a risk level. */
export function riskLabel(level: Risk): string {
  return RISK_LABELS[level];
}
