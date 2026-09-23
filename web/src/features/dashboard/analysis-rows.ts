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
import { severityRank } from "@/lib/format/severity-color";
import { isConfidence, type Confidence } from "@/features/alerts/analysis/enums";
import type { Record_ } from "@/features/alerts/types";
import { analysisEpoch } from "@/features/alerts/analysis/time";
import {
  analysisIsStale,
  authorOf,
  isPlanStatus,
  isStepWhen,
  readCaveats,
  splitSummary,
  type Author,
  type PlanStatus,
  type StepWhen,
} from "@/features/alerts/analysis/verdict";

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

/**
 * How many alerts the alerts page loads per page (its `PAGE_SIZE`).
 *
 * Duplicated rather than imported because the alerts page does not export it;
 * `analysis-rows.test.ts` reads the page's source and fails the day the two
 * disagree. It matters because of {@link openAlertSearch}.
 */
export const ALERTS_PAGE_SIZE = 50;

/** The analysed population, in the alerts page's search DSL. */
export function analysedSetSearch(): string {
  return encodeText(ANALYSED_OPEN_ALERTS);
}

/**
 * The `?search=` an opened row lands the alerts page on.
 *
 * Preferably the whole analysed set: the inspector's prev/next then walks the
 * analysed alerts — the queue the reader came from — instead of reading "1 / 1"
 * on a table pinned to one row. That is only safe while the set fits on the
 * page the alerts table loads: it fetches ONE page (offset 0, `PAGE_SIZE` rows,
 * in its own `date_epoch` order, not this view's), and a drawer whose uid is
 * not on that page closes itself as a stale link. At or under a page, every
 * analysed alert is on page 1 whatever the order, so the target is too. Past
 * it, fall back to pinning the one uid — a working drawer with no neighbours
 * beats a neighbourly one that never opens.
 *
 * `analysedTotal` is the server's `meta.total` for the population, not the
 * length of the list on screen: a filtered view still lands on the full set.
 */
export function openAlertSearch(uid: string, analysedTotal: number): string {
  return analysedTotal <= ALERTS_PAGE_SIZE ? analysedSetSearch() : uidSearch(uid);
}

/** One remediation step, as the view shows it. */
export type PlanStep = {
  /** What to do. The one part a step is useless without. */
  action: string;
  /** The exact command, when the step is one. Empty for "open an MR"-shaped steps. */
  command: string;
  /** `low` | `medium` | `high`, or "" when the subtree named something else. */
  risk: RiskLevel | "";
  /** `now` | `follow_up`, or undefined when the step does not say (older analyses). */
  when: StepWhen | undefined;
};

/** The risk levels a step's badge knows how to paint. */
export const RISK_LEVELS = ["low", "medium", "high"] as const;
export type RiskLevel = (typeof RISK_LEVELS)[number];

function isRisk(value: unknown): value is RiskLevel {
  return typeof value === "string" && (RISK_LEVELS as readonly string[]).includes(value);
}

/**
 * How many steps carry a risk worth printing. Low is the default answer and is
 * never marked (the shared risk rule with the inspector), so the count is of
 * the steps that change who may run them.
 */
export function riskyStepCount(plan: readonly PlanStep[]): number {
  return plan.filter((s) => s.risk === "medium" || s.risk === "high").length;
}

/** The row shape the view renders — one analysed alert, already parsed. */
export type AnalysedRow = {
  /** Record uid; the deep link's whole point, so a row without one is dropped. */
  uid: string;
  /** Raw severity label, for the badge, the rail and the urgency sort. */
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
  /**
   * The alert's lifecycle state, raw (`""` reads as open). The population
   * deliberately keeps acknowledged rows, so the row has to say which it is.
   */
  state: string;
  /** Whether a snooze filter is holding the alert (`snoozed` names the filter). */
  snoozed: boolean;
  /** `date_epoch` — the last time the alert fired (bumped on every refire). */
  firedAt: number | undefined;
  /** `root_cause.summary` as written, or "—" when there isn't one. */
  summary: string;
  /** The line a triager reads first (see `splitSummary`). */
  headline: string;
  /** Everything behind the headline; "" when the summary is the whole story. */
  body: string;
  /** The limits of the investigation, legacy "caveat:" evidence folded in. */
  caveats: string[];
  /** Absent when the subtree carries no level this app knows: the meter is omitted. */
  confidence: Confidence | undefined;
  /** The plan's verdict, when the analysis states one. */
  status: PlanStatus | undefined;
  /** How many steps the remediation plan carries. */
  steps: number;
  /**
   * Those steps, parsed. The count is kept beside them because it is what a
   * plan whose `steps` is malformed can still report honestly.
   */
  plan: PlanStep[];
  automatable: boolean;
  /** `analysis.at` as epoch seconds, for TimeCell. */
  analysedAt: number | undefined;
  /** `analysis.by` — the identity that wrote it (an agent login, or a human). */
  by: string;
  /** Who wrote it, as the reader needs to know it: a tool, or a person. */
  author: Author;
  /** The alert fired again after the analysis was written. */
  stale: boolean;
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
  // features/alerts/analysis/time.ts so the inspector and this view can never
  // disagree about which stamps are worth showing.
  return analysisEpoch(at);
}

/** What a missing cause reads as, matching the app's other empty cells. */
const EM_DASH = "—";

/**
 * toAnalysedRow parses one record into a view row.
 *
 * It returns undefined only for a record this view was never counting — no
 * uid to link, or no `agentic` subtree at all. Everything else becomes a row,
 * however malformed: the Analysed tile counts the server's `meta.total` over
 * the same `EXISTS agentic` predicate, so dropping a parsed-but-odd record here
 * would print a number larger than the list it opens. A degraded row says what
 * it knows and leaves the rest blank, which is at least a link to the alert.
 *
 * The fields the contract grew later (`detail`, `caveats`, `status`, `when`)
 * are all optional and all read loosely: the analyses already stored predate
 * them, and the view must read those exactly as well as it did before.
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
  const { headline, body } = splitSummary(summary, asString(rootCause?.["detail"]));
  const { caveats } = readCaveats(rootCause);

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
        const whenRaw = step?.["when"];
        return {
          action: asString(step?.["action"]) || asString(raw),
          command: asString(step?.["command"]),
          risk: isRisk(riskRaw) ? riskRaw : "",
          when: isStepWhen(whenRaw) ? whenRaw : undefined,
        };
      })
    : [];
  const statusRaw = plan?.["status"];
  const analysis = asObject(agentic["analysis"]);

  // `labels` is an ingest-side map (Prometheus/AlertManager and friends put the
  // rule name in `labels.alertname`); `process` is the native Snooze field for
  // the same "what fired" slot.
  const labels = asObject(record["labels"]);
  const alertname = asString(labels?.["alertname"]) || asString(record.process);
  const message = asString(record.message);
  const firedRaw: unknown = record.date_epoch;
  const firedAt =
    typeof firedRaw === "number" && Number.isFinite(firedRaw) && firedRaw > 0
      ? firedRaw
      : undefined;

  return {
    uid,
    severity: asString(record.severity),
    host: asString(record.host),
    message,
    alertname,
    state: asString(record.state),
    // `snoozed` names the filter holding the alert; any non-empty value is a hold.
    snoozed: Boolean(record["snoozed"]),
    firedAt,
    summary: summary || EM_DASH,
    headline: headline || EM_DASH,
    body,
    caveats,
    confidence,
    status: isPlanStatus(statusRaw) ? statusRaw : undefined,
    steps,
    plan: planRows,
    automatable: plan?.["automatable"] === true,
    analysedAt: analysedAtEpoch(analysis?.["at"]),
    by: asString(analysis?.["by"]),
    author: authorOf(analysis),
    stale: analysisIsStale(firedAt, analysis?.["at"]),
  };
}

/** The automatable filter's three positions. */
export type AutomatableFilter = "any" | "yes" | "no";

/**
 * The confidence filter: a threshold, not a set. "Show me what I can lean on"
 * is the question — nobody filters for "low and high but not medium", and a
 * three-way multi-select that starts all-on made its own state unreadable.
 */
export type ConfidenceFilter = "any" | "medium" | "high";

/** The verdict filter: one plan status, or all of them. */
export type VerdictFilter = "any" | PlanStatus;

export type AnalysisFilters = {
  confidence: ConfidenceFilter;
  automatable: AutomatableFilter;
  verdict: VerdictFilter;
};

/** Nothing narrowed — every row the server counted. */
export const ANY_FILTERS: AnalysisFilters = {
  confidence: "any",
  automatable: "any",
  verdict: "any",
};

export function filtersActive(f: AnalysisFilters): boolean {
  return f.confidence !== "any" || f.automatable !== "any" || f.verdict !== "any";
}

const CONFIDENCE_FLOOR: Record<Exclude<ConfidenceFilter, "any">, readonly Confidence[]> = {
  medium: ["high", "medium"],
  high: ["high"],
};

/**
 * matchesFilters applies the view's local filters to one row.
 *
 * A degraded row (no level, no verdict) survives only the "Any" position of
 * the question it cannot answer: an operator who asked for "Medium+" is asking
 * something this row does not say, and one who has not narrowed at all should
 * see everything the count counted.
 */
export function matchesFilters(row: AnalysedRow, f: AnalysisFilters): boolean {
  if (f.confidence !== "any") {
    if (row.confidence === undefined || !CONFIDENCE_FLOOR[f.confidence].includes(row.confidence)) {
      return false;
    }
  }
  if (f.automatable === "yes" && !row.automatable) return false;
  if (f.automatable === "no" && row.automatable) return false;
  if (f.verdict !== "any" && row.status !== f.verdict) return false;
  return true;
}

/**
 * The two orders the view offers. `urgent` is the default and is omitted from
 * the URL; `recent` round-trips as `?sort=recent`.
 */
export type AnalysesSort = "urgent" | "recent";

/**
 * Whether somebody already has the alert in hand: acknowledged, or held by a
 * snooze filter. Re-escalated is NOT held — escalation is the alert coming back
 * to the queue after an acknowledgement ran out.
 */
function isHeld(row: AnalysedRow): boolean {
  return row.state === "ack" || row.snoozed;
}

/** Descending on a number that may be missing; missing sorts last. */
function desc(a: number | undefined, b: number | undefined): number {
  return (b ?? -Infinity) - (a ?? -Infinity);
}

function compareUrgent(a: AnalysedRow, b: AnalysedRow): number {
  // The severity ladder is the app's own (server-installed, with the built-in
  // syslog fallback): LOWER is worse. An unrecognised label is not evidence of
  // urgency, so it sorts after every known one.
  const sev = (severityRank(a.severity) ?? Infinity) - (severityRank(b.severity) ?? Infinity);
  if (sev !== 0 && !Number.isNaN(sev)) return sev;
  const held = Number(isHeld(a)) - Number(isHeld(b));
  if (held !== 0) return held;
  return desc(a.firedAt, b.firedAt);
}

function compareRecent(a: AnalysedRow, b: AnalysedRow): number {
  return desc(a.analysedAt, b.analysedAt);
}

/**
 * sortRows orders the fetched rows client-side.
 *
 * "Most urgent" is the default because the view is a work queue: the severity
 * the alert fired at, then whether anybody has it in hand, then how recently it
 * fired. "Newest analysis" is the question "what did the agent just write".
 * Ties fall back to the uid so a 30-second refetch never shuffles equal rows
 * under the reader's eye.
 */
export function sortRows(rows: readonly AnalysedRow[], sort: AnalysesSort): AnalysedRow[] {
  const compare = sort === "recent" ? compareRecent : compareUrgent;
  return [...rows].sort((a, b) => compare(a, b) || (a.uid < b.uid ? -1 : a.uid > b.uid ? 1 : 0));
}
