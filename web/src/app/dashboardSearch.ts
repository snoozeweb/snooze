// The dashboard route's search-param contract, kept apart from router.tsx for
// the same reason as alertsSearch.ts: a test can build a route with the real
// validator without importing the router module (which arms the background
// token refresh on load).
//
// `view` picks which of the page's two views is on screen; `range` is the time
// picker's preset key and, for the "custom" preset, `from`/`to` carry the
// window bounds as epoch milliseconds. The rest belongs to the Analyses view:
// `sort` is its order (omitted = "most urgent", `recent` = newest analysis
// first) and `confidence` / `automatable` / `verdict` / `closed` are its
// filters. Every key is optional and every default is the ABSENCE of its key,
// so no params means the Overview on its default 1d range and the Analyses
// view on its default filters. Types are validated defensively (numeric
// strings coerced to number, unknown values dropped) so a hand-edited URL can't
// poison the page.
//
// No imports on purpose: router.tsx loads eagerly, and the dashboard's modules
// are a lazy chunk. The verdict ids are therefore spelled out here;
// dashboardSearch.test.ts fails the day they drift from PLAN_STATUSES.

export type DashboardSearchParams = {
  view?: "overview" | "analyses";
  sort?: "recent";
  range?: "1d" | "1w" | "1m" | "1y" | "custom";
  from?: number;
  to?: number;
  /** Confidence threshold; absent = any. */
  confidence?: "medium" | "high";
  /** Automatable filter; absent = any. */
  automatable?: "yes" | "no";
  /**
   * The verdicts shown, comma-separated in {@link VERDICT_IDS} order; absent =
   * the default set ({@link DEFAULT_VERDICT_IDS}). An empty string is a real
   * state: every verdict unticked.
   */
  verdict?: string;
  /** `hide` drops closed alerts from the list; absent = show them. */
  closed?: "hide";
};

/**
 * Every verdict option the filter offers: the plan statuses, in their severity
 * order, then `no_verdict` for analyses that state none (every analysis written
 * before the field existed, or on the legacy schema fallback).
 */
export const VERDICT_IDS = [
  "action_required",
  "monitoring",
  "self_resolved",
  "resolved",
  "no_verdict",
] as const;
export type VerdictId = (typeof VERDICT_IDS)[number];

/**
 * What the view opens on: the verdicts that still ask something of a person.
 * `monitoring` needs somebody to confirm the watch came back green, and an
 * analysis with no verdict has not said it is finished — hiding it would drop
 * it silently. The finished verdicts are one click away.
 */
export const DEFAULT_VERDICT_IDS: readonly VerdictId[] = [
  "action_required",
  "monitoring",
  "no_verdict",
];

function isVerdictId(value: string): value is VerdictId {
  return (VERDICT_IDS as readonly string[]).includes(value);
}

/** Dedupes and orders a verdict set the one way the URL spells it. */
export function canonicalVerdicts(ids: Iterable<string>): VerdictId[] {
  const set = new Set(ids);
  return VERDICT_IDS.filter((id) => set.has(id));
}

/** Whether a verdict set is the default one (and so is omitted from the URL). */
export function isDefaultVerdicts(ids: readonly VerdictId[]): boolean {
  const canonical = canonicalVerdicts(ids);
  return (
    canonical.length === DEFAULT_VERDICT_IDS.length &&
    canonical.every((id, i) => id === DEFAULT_VERDICT_IDS[i])
  );
}

/**
 * Reads `?verdict=`. Undefined means "the default": the key is absent, not a
 * string, or names nothing this app knows (a typo must not empty the list).
 * An empty string is the one spelling of "nothing ticked".
 */
export function parseVerdictParam(raw: unknown): VerdictId[] | undefined {
  if (typeof raw !== "string") return undefined;
  if (raw.trim() === "") return [];
  const known = canonicalVerdicts(
    raw
      .split(",")
      .map((s) => s.trim())
      .filter(isVerdictId),
  );
  return known.length === 0 ? undefined : known;
}

export function validateDashboardSearch(raw: Record<string, unknown>): DashboardSearchParams {
  const out: DashboardSearchParams = {};
  // Anything but the one named view falls through to the Overview — the
  // param is omitted from the URL in that case, so `?view=overview` and no
  // param at all are the same state.
  if (raw["view"] === "analyses") out.view = "analyses";
  // Same rule for the sort: the default ("most urgent") is the absence of
  // the param, so only the one alternative is ever kept.
  if (raw["sort"] === "recent") out.sort = "recent";
  const rangeRaw = raw["range"];
  if (
    rangeRaw === "1d" ||
    rangeRaw === "1w" ||
    rangeRaw === "1m" ||
    rangeRaw === "1y" ||
    rangeRaw === "custom"
  ) {
    out.range = rangeRaw;
  }
  const num = (k: string) => {
    const v = raw[k];
    if (typeof v === "number" && Number.isFinite(v)) return v;
    if (typeof v === "string" && /^\d+$/.test(v)) return Number(v);
    return undefined;
  };
  const from = num("from");
  if (from !== undefined) out.from = from;
  const to = num("to");
  if (to !== undefined) out.to = to;

  // The Analyses filters: only a non-default value is ever kept.
  const confidence = raw["confidence"];
  if (confidence === "medium" || confidence === "high") out.confidence = confidence;
  const automatable = raw["automatable"];
  if (automatable === "yes" || automatable === "no") out.automatable = automatable;
  const verdicts = parseVerdictParam(raw["verdict"]);
  if (verdicts !== undefined && !isDefaultVerdicts(verdicts)) out.verdict = verdicts.join(",");
  if (raw["closed"] === "hide") out.closed = "hide";
  return out;
}
