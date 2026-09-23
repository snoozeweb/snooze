// Reading the agentic analysis off an alert record, for the dashboard's
// Analyses panel.
//
// It lives here rather than in `features/alerts/analysis/` because that folder
// is the inspector's surface: it knows the analysis of ONE alert, fetched from
// /record/{uid}/agentic. The panel reads the same subtree from the other end —
// the copy the record itself carries in a list response — so the parsing is the
// panel's problem, not the tab's.
//
// Everything below is defensive on purpose. `agentic` is a protected subtree
// the generic Record schema does not declare (records are dynamic and the
// subtree is only ever written through its own route), so it arrives as an
// untyped extra field. It is parsed leniently rather than strictly: the panel
// lists what the server counted, so a record dropped here would be counted by
// the Analysed tile and then missing from the list under it. Anything carrying
// an `agentic` subtree becomes a row; the parts that are missing degrade to an
// em-dash cause, an em-dash plan and an absent badge.
import type { Condition } from "@/lib/condition/types";
import { encodeText } from "@/lib/condition/text";
import { CONFIDENCE_LEVELS, isConfidence, type Confidence } from "@/features/alerts/analysis/enums";
import type { Record_ } from "@/features/alerts/types";
import { analysisEpoch } from "@/features/alerts/analysis/time";

/**
 * The alerts this view is about, as one population expressed two ways.
 *
 * `OPEN_CLAUSES` is the alerts page's ACTIVE_ALERTS preset minus two of its
 * clauses:
 *
 *   - the snoozed clause, because an analysed alert that a snooze filter is
 *     currently silencing is still an alert somebody worked out the cause of,
 *     and hiding it here would make the panel disagree with itself the moment
 *     a filter expires;
 *   - the acknowledged clause, because acknowledging is what an operator does
 *     *after* reading the analysis — dropping acked rows would empty the panel
 *     exactly when it starts being useful.
 *
 * What is left is "not finished with": not closed, not shelved, and not
 * permanently shelved through the legacy negative TTL.
 *
 * The list (`ANALYSED_OPEN_ALERTS`) and the tile's denominator (`OPEN_ALERTS`)
 * are both built from this one array, because they are read against each
 * other. Measuring the denominator with a different predicate is how
 * "7 analysed of 6 open" — the ordinary end state, since ACTIVE_ALERTS drops
 * the acked and snoozed rows the numerator deliberately keeps — used to be
 * printed as a ratio.
 */
const OPEN_CLAUSES: Condition[] = [
  { type: "NOT", arg: { type: "EQUALS", field: "state", value: "close" } },
  { type: "NOT", arg: { type: "EQUALS", field: "state", value: "shelved" } },
  { type: "NOT", arg: { type: "LT", field: "ttl", value: 0 } },
];

/** The population: every alert still in play, acknowledged and snoozed included. */
export const OPEN_ALERTS: Condition = { type: "AND", args: [...OPEN_CLAUSES] };

/**
 * Its analysed share.
 *
 * Deliberately NOT narrowed to rows carrying `agentic.analysis.at`: the list is
 * sorted by that path, and the three backends disagree on where a missing value
 * sorts (Postgres puts NULLs first on DESC, SQLite and Mongo last), so a
 * subtree written without a timestamp can lead the list. Excluding it would fix
 * the ordering by hiding rows the count still counts — the divergence this
 * module exists to prevent. The row parser degrades them instead.
 */
export const ANALYSED_OPEN_ALERTS: Condition = {
  type: "AND",
  args: [{ type: "EXISTS", field: "agentic" }, ...OPEN_CLAUSES],
};

/**
 * The search text that pins the alerts table to one record.
 *
 * A row's deep link carries `?record=<uid>`, but the alerts page fetches one
 * page of the newest alerts by `date_epoch` and closes a drawer whose uid is
 * not among them — which is exactly the case this view creates: an old alert
 * explained an hour ago sorts first here and nowhere near page 1 there. The
 * search pins it, so the target is always on the page it lands on.
 */
export function uidSearch(uid: string): string {
  return encodeText({ type: "EQUALS", field: "uid", value: uid });
}

/** One remediation step, as the panel shows it beside the cause. */
export type PlanStep = {
  /** What to do. The one part a step is useless without. */
  action: string;
  /** The exact command, when the step is one. Empty for "open an MR"-shaped steps. */
  command: string;
  /** `low` | `medium` | `high`, or "" when the subtree named something else. */
  risk: RiskLevel | "";
};

/** The risk levels a step's badge knows how to paint. */
export const RISK_LEVELS = ["low", "medium", "high"] as const;
export type RiskLevel = (typeof RISK_LEVELS)[number];

function isRisk(value: unknown): value is RiskLevel {
  return typeof value === "string" && (RISK_LEVELS as readonly string[]).includes(value);
}

/** The row shape the panel renders — one analysed alert, already parsed. */
export type AnalysedRow = {
  /** Record uid; the deep link's whole point, so a row without one is dropped. */
  uid: string;
  /** Raw severity label, for the leading dot. */
  severity: string;
  host: string;
  /**
   * The alert's own message — what actually fired, in the words the source
   * sent. It leads the row over `labels.alertname`, which is the rule's
   * identifier: "NodeFilesystemAlmostOutOfSpace" names a rule, "/var at 94%"
   * names the problem.
   */
  message: string;
  /** `labels.alertname`, falling back to the record's process. The fallback
   *  when a record carries no message of its own. */
  alertname: string;
  /** `root_cause.summary` — the one-line cause, or "—" when there isn't one. */
  summary: string;
  /** Absent when the subtree carries no level this app knows: the badge is omitted. */
  confidence: Confidence | undefined;
  /** How many steps the remediation plan carries. */
  steps: number;
  /**
   * Those steps, parsed. The panel shows the plan beside the cause, so the
   * count alone is no longer enough — but it is kept, because the count is what
   * a plan whose `steps` is malformed can still report honestly.
   */
  plan: PlanStep[];
  automatable: boolean;
  /** `analysis.at` as epoch seconds, for TimeCell. */
  analysedAt: number | undefined;
  /** `analysis.by` — the identity that wrote it (an agent login, or a human). */
  by: string;
};

function asObject(value: unknown): Record<string, unknown> | undefined {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return undefined;
  return value as Record<string, unknown>;
}

function asString(value: unknown): string {
  return typeof value === "string" ? value : "";
}

/**
 * `analysis.at` is RFC3339 (the OpenAPI `date-time` format); every timestamp in
 * this app renders from epoch SECONDS through TimeCell, which owns the
 * relative/absolute pair and the tooltip. Convert at the edge rather than
 * teaching TimeCell a second input shape.
 */
export function analysedAtEpoch(at: unknown): number | undefined {
  // One converter for every surface that reads `analysis.at`: the sanity
  // window (Go's zero time, bare years, far-future stamps) lives in
  // features/alerts/analysis/time.ts so the inspector and this table can never
  // disagree about which stamps are worth showing.
  return analysisEpoch(at);
}

/** What a missing cause reads as, matching the table's other empty cells. */
const EM_DASH = "\u2014";

/**
 * toAnalysedRow parses one record into a panel row.
 *
 * It returns undefined only for a record this panel was never counting — no
 * uid to link, or no `agentic` subtree at all. Everything else becomes a row,
 * however malformed: the count above the list is the server's `meta.total` over
 * the same `EXISTS agentic` predicate, so dropping a parsed-but-odd record here
 * would print a number larger than the list under it. A degraded row says what
 * it knows and leaves the rest blank, which is at least a link to the alert.
 */
export function toAnalysedRow(record: Record_): AnalysedRow | undefined {
  const uid = asString(record.uid);
  if (uid === "") return undefined;

  // Truthiness, not shape: the predicate that counted this record was
  // `EXISTS agentic`, so anything the server considered present counts here.
  const agenticRaw = record["agentic"];
  if (agenticRaw === undefined || agenticRaw === null || agenticRaw === "") return undefined;
  const agentic = asObject(agenticRaw) ?? {};

  const rootCauseRaw = agentic["root_cause"];
  const rootCause = asObject(rootCauseRaw);
  const confidenceRaw = rootCause?.["confidence"];
  const confidence = isConfidence(confidenceRaw) ? confidenceRaw : undefined;
  // A hand-written subtree sometimes puts the sentence where the object goes.
  const summary =
    asString(rootCause?.["summary"]) || asString(rootCauseRaw) || asString(agentic["summary"]);

  const plan = asObject(agentic["remediation_plan"]);
  const planSteps = plan?.["steps"];
  const steps = Array.isArray(planSteps) ? planSteps.length : 0;
  // Same leniency as everything else here: a step that is a bare string (a
  // hand-written subtree) becomes its own action, and one that names no action
  // at all still occupies its position in the plan rather than silently
  // shortening it — the count beside it is the array's length.
  const planRows: PlanStep[] = Array.isArray(planSteps)
    ? planSteps.map((raw) => {
        const step = asObject(raw);
        const riskRaw = step?.["risk"];
        return {
          action: asString(step?.["action"]) || asString(raw),
          command: asString(step?.["command"]),
          risk: isRisk(riskRaw) ? riskRaw : "",
        };
      })
    : [];
  const analysis = asObject(agentic["analysis"]);

  // `labels` is an ingest-side map (Prometheus/AlertManager and friends put the
  // rule name in `labels.alertname`); `process` is the native Snooze field for
  // the same "what fired" slot.
  const labels = asObject(record["labels"]);
  const alertname = asString(labels?.["alertname"]) || asString(record.process);
  const message = asString(record.message);

  return {
    uid,
    severity: asString(record.severity),
    host: asString(record.host),
    message,
    alertname,
    summary: summary || EM_DASH,
    confidence,
    steps,
    plan: planRows,
    automatable: plan?.["automatable"] === true,
    analysedAt: analysedAtEpoch(analysis?.["at"]),
    by: asString(analysis?.["by"]),
  };
}

/** The automatable chip's three positions. */
export type AutomatableFilter = "any" | "yes" | "no";

/**
 * matchesFilters applies the panel's two local filters to one row.
 *
 * Deselecting every confidence is taken literally — it matches nothing, and the
 * panel says so — rather than silently meaning "all". A filter strip that
 * quietly ignores the operator is worse than an empty list they can undo with
 * one click.
 *
 * A degraded row (no level in its subtree) is not any of the three levels, so
 * it survives only while the chips are untouched: an operator who has asked for
 * "high and medium" is asking a question this row cannot answer, and one who
 * has not narrowed at all should see everything the count counted.
 */
export function matchesFilters(
  row: AnalysedRow,
  confidences: readonly Confidence[],
  automatable: AutomatableFilter,
): boolean {
  if (row.confidence === undefined) {
    if (confidences.length !== CONFIDENCE_LEVELS.length) return false;
  } else if (!confidences.includes(row.confidence)) {
    return false;
  }
  if (automatable === "yes" && !row.automatable) return false;
  if (automatable === "no" && row.automatable) return false;
  return true;
}
