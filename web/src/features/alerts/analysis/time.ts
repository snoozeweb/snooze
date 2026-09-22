// Reading `analysis.at` off a stored agentic subtree.
//
// It lives in its own file rather than inside ProvenanceLine so a surface that
// only needs the number — the dashboard's Analyses table goes through
// `features/dashboard/analysis-rows.ts` — can import it without pulling a
// component and TimeCell into its graph.

/**
 * The shape the server writes: `time.RFC3339` in UTC. Checked before parsing
 * because `Date.parse` is far more generous than the contract — it reads a
 * bare "2026" as January 1st and reports it as a timestamp hundreds of days
 * old, which reads exactly like a real (and wrong) provenance line.
 */
const RFC3339 = /^\d{4}-\d{2}-\d{2}[Tt ]\d{2}:\d{2}:\d{2}/;

/**
 * Nothing in this product was analysed before 2000. The value this floor
 * really catches is Go's zero time, `0001-01-01T00:00:00Z`, which any code
 * path that marshalled an unstamped `time.Time` produces — it parses cleanly
 * and renders as "739879d ago".
 */
const FLOOR_MS = Date.UTC(2000, 0, 1);

/**
 * A stamp is allowed to be slightly ahead of this browser's clock — cluster
 * members and laptops drift — but not by a day. Beyond that it is bad data,
 * and "in 3 years" under a root cause is worse than no time at all.
 */
const FUTURE_SLACK_MS = 24 * 60 * 60 * 1000;

/**
 * analysisEpoch converts `analysis.at` into the epoch SECONDS every timestamp
 * in this app renders from (TimeCell owns the relative/absolute pair and the
 * tooltip), or undefined when the value is not a timestamp worth showing.
 *
 * Takes `unknown`: the agentic subtree is loose JSON on a dynamic record, so
 * `at` is whatever was stored, not necessarily a string.
 */
export function analysisEpoch(at: unknown): number | undefined {
  if (typeof at !== "string" || !RFC3339.test(at)) return undefined;
  const ms = Date.parse(at);
  if (Number.isNaN(ms)) return undefined;
  if (ms < FLOOR_MS || ms > Date.now() + FUTURE_SLACK_MS) return undefined;
  return Math.floor(ms / 1000);
}
