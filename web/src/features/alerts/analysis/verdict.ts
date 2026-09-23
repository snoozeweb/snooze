// The verdict layer of an agentic analysis: what the on-call engineer reads
// first, before any justification.
//
// Every surface that shows an analysis — the drawer's Analysis tab, its header
// line, the dashboard's Analyses rows — answers the same question in the same
// order: is there anything to do (plan status), what happened in one line (the
// headline), how far to trust it (confidence, caveats, freshness), and only
// then why (detail, evidence). This file owns the readings those surfaces
// share, so the dashboard and the drawer cannot disagree about what an
// analysis says.
//
// Everything here reads loose JSON: the subtree is stored on a dynamic record
// and may predate the fields it now carries (`detail`, `caveats`, `status`,
// `when`). An older analysis degrades to what it has — never to an error.
import { analysisEpoch } from "./time";
import { ANALYSIS_SOURCE } from "./schema";

/** Accepted values of `remediation_plan.status`, in severity order. */
export const PLAN_STATUSES = ["action_required", "monitoring", "self_resolved"] as const;
export type PlanStatus = (typeof PLAN_STATUSES)[number];

/** Accepted values of a step's `when`. */
export const STEP_WHENS = ["now", "follow_up"] as const;
export type StepWhen = (typeof STEP_WHENS)[number];

const PLAN_STATUS_LABELS: Record<PlanStatus, string> = {
  action_required: "Action required",
  monitoring: "Monitoring",
  self_resolved: "Self-resolved",
};

/** What each verdict asks of the person reading it, in their words. */
const PLAN_STATUS_HINTS: Record<PlanStatus, string> = {
  action_required: "On-call needs to act on this alert",
  monitoring: "Nothing to run yet — keep watching",
  self_resolved: "Recovered on its own — safe to close once confirmed",
};

const STEP_WHEN_LABELS: Record<StepWhen, string> = {
  now: "Now",
  follow_up: "Follow-up",
};

export function isPlanStatus(value: unknown): value is PlanStatus {
  return typeof value === "string" && (PLAN_STATUSES as readonly string[]).includes(value);
}

export function isStepWhen(value: unknown): value is StepWhen {
  return typeof value === "string" && (STEP_WHENS as readonly string[]).includes(value);
}

export function planStatusLabel(status: PlanStatus): string {
  return PLAN_STATUS_LABELS[status];
}

export function planStatusHint(status: PlanStatus): string {
  return PLAN_STATUS_HINTS[status];
}

export function stepWhenLabel(when: StepWhen): string {
  return STEP_WHEN_LABELS[when];
}

/**
 * The headline budget. A summary at or under it is a headline already; above
 * it the contract has been ignored (the API asks for one sentence) and the
 * renderer recovers a headline instead of setting a paragraph in display type.
 */
const HEADLINE_SOFT = 160;
/** Longest first sentence still worth promoting whole to the headline. */
const HEADLINE_HARD = 180;

/**
 * A sentence end: terminal punctuation, whitespace, then something that
 * starts a sentence. Lowercase after the stop ("e.g. on ovh") and digits
 * glued to it ("api:3.7.0") are not sentence ends.
 */
const SENTENCE_END = /[.!?](?=\s+[A-Z(“"'])/g;

/**
 * splitSummary turns `summary` (+ optional `detail`) into the headline a
 * triager reads and the body underneath it.
 *
 * With `detail` present the contract is being followed: summary is the
 * headline, detail the body. Without it, a short summary stands alone and a
 * long one is split at its first sentence — or, when even that sentence runs
 * past the budget, clipped at a word, with the body picking up from the clip
 * ("…rest") so nothing the agent wrote is lost and nothing is said twice.
 */
export function splitSummary(summary: string, detail?: string): { headline: string; body: string } {
  const s = summary.trim();
  const d = (detail ?? "").trim();
  if (d !== "") return { headline: s, body: d };
  if (s.length <= HEADLINE_SOFT) return { headline: s, body: "" };

  SENTENCE_END.lastIndex = 0;
  const match = SENTENCE_END.exec(s);
  if (match && match.index + 1 <= HEADLINE_HARD) {
    const cut = match.index + 1;
    return { headline: s.slice(0, cut), body: s.slice(cut).trim() };
  }
  const window = s.slice(0, HEADLINE_HARD);
  const space = window.lastIndexOf(" ");
  const clipped = (space > HEADLINE_SOFT / 2 ? window.slice(0, space) : window).replace(
    /[\s,;:–—-]+$/,
    "",
  );
  return { headline: `${clipped}…`, body: `…${s.slice(clipped.length).trimStart()}` };
}

/** Legacy spelling: agents that predate `caveats` filed them as evidence. */
const CAVEAT_PREFIX = /^\s*caveats?\s*:\s*/i;

function strings(value: unknown): string[] {
  return Array.isArray(value)
    ? value.filter((v): v is string => typeof v === "string" && v.trim() !== "")
    : [];
}

/**
 * readCaveats separates the limits of an investigation from its evidence:
 * explicit `caveats` first, then any evidence line an older agent prefixed
 * with "caveat:" (prefix stripped). Caveats are read before a conclusion is
 * trusted, so they must not sit at the bottom of the evidence list.
 */
export function readCaveats(rootCause: { evidence?: unknown; caveats?: unknown } | undefined): {
  evidence: string[];
  caveats: string[];
} {
  const evidence: string[] = [];
  const caveats = strings(rootCause?.caveats);
  for (const item of strings(rootCause?.evidence)) {
    if (CAVEAT_PREFIX.test(item)) caveats.push(item.replace(CAVEAT_PREFIX, ""));
    else evidence.push(item);
  }
  return { evidence, caveats };
}

export type IndexedStep<T> = { step: T; index: number };

/**
 * groupSteps sorts a plan into what on-call runs now and what is follow-up
 * work, keeping each step's original index (the number an operator quotes).
 * A plan where no step says `when` is ungrouped: every step is "now" and the
 * surfaces render one list, as before the field existed.
 */
export function groupSteps<T extends { when?: unknown }>(
  steps: readonly T[],
): { grouped: boolean; now: IndexedStep<T>[]; followUp: IndexedStep<T>[] } {
  const now: IndexedStep<T>[] = [];
  const followUp: IndexedStep<T>[] = [];
  let grouped = false;
  steps.forEach((step, index) => {
    if (isStepWhen(step.when)) grouped = true;
    (step.when === "follow_up" ? followUp : now).push({ step, index });
  });
  return { grouped, now, followUp };
}

export type Author = { kind: "agent" | "human"; tool: string; by: string };

/**
 * authorOf says who wrote an analysis. The web editor stamps
 * `source: snooze-web`, so that — and only that — is a human edit. Everything
 * else arrived through the API from a tool (alert-rca, the MCP server, the
 * CLI an agent drives) and is labelled an AI analysis: an unlabelled machine
 * conclusion is read with a human's authority, which is the one mistake this
 * label exists to prevent.
 */
export function authorOf(meta: { source?: unknown; by?: unknown } | undefined): Author {
  const source = typeof meta?.source === "string" ? meta.source : "";
  const by = typeof meta?.by === "string" ? meta.by : "";
  if (source === ANALYSIS_SOURCE) return { kind: "human", tool: "", by };
  return { kind: "agent", tool: source, by };
}

/** Clock skew between the analyser, the server and this browser. */
const STALE_SLACK_S = 60;

/**
 * analysisIsStale: the alert has fired again since the analysis was written,
 * so the conclusion may describe an earlier incident. `lastEpoch` is the
 * record's `date_epoch` (bumped on every refire).
 */
export function analysisIsStale(lastEpoch: unknown, at: unknown): boolean {
  const written = analysisEpoch(at);
  if (typeof lastEpoch !== "number" || !Number.isFinite(lastEpoch) || written === undefined)
    return false;
  return lastEpoch > written + STALE_SLACK_S;
}
