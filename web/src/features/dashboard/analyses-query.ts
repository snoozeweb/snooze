// The queries behind the dashboard's two agentic-analysis surfaces: the
// "Analysed" tile in the Right-now cluster and the Analyses view itself.
//
// Three hooks, not one. The tile prints a single integer and is on screen the
// whole time the Overview is, so it asks for one row and reads `meta.total`;
// pulling 50 full alert records every 30 seconds to print that integer is what
// the tile used to cost. The list is the view's own, and only mounts with it.
// Both are the SAME predicate (`ANALYSED_OPEN_ALERTS`), so their totals agree —
// they are two page sizes over one population, not two questions.
//
// The third is the denominator: the same population minus the analysis clause.
// It has to be measured here rather than borrowed from the alerts page's
// ACTIVE_ALERTS probe, which drops the acknowledged and snoozed rows this
// numerator deliberately keeps — the two together printed "7 analysed of
// 6 open" as the ordinary end state.
import type { UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "@/lib/api/client";
import type { ListResponse } from "@/lib/api/resource";
import { encodeConditionQ } from "@/lib/condition/serialize";
import { Records } from "@/features/alerts/api";
import type { Record_ } from "@/features/alerts/types";
import { ANALYSED_OPEN_ALERTS, OPEN_ALERTS } from "./analysis-rows";

/**
 * How many rows the view fetches.
 *
 * The view lists every analysed alert — it is a work queue, not a top-N — so
 * this is a safety ceiling rather than a page size: high enough that a real
 * backlog fits under it whole, finite so a runaway analyser cannot ask the
 * browser to lay out ten thousand two-column rows. The view says so out loud on
 * the only occasion it bites (`analysed` above `rows.length`).
 */
export const ANALYSES_ROW_CAP = 500;

/** The dashboard's poll cadence — every other live panel repaints on it. */
const REFRESH_MS = 30_000;

type CountQuery = UseQueryResult<ListResponse<Record_>, ApiError>;

/**
 * How many open alerts carry an analysis — one row fetched, `meta.total` read.
 *
 * `enabled` is the permission gate: this is a `/record` query, so an identity
 * holding only `ro_stats` (enough for the dashboard itself) gets a 403 from it.
 * Left on, it would 403 every 30 seconds behind a tile that silently never
 * appears.
 */
export function useAnalysedCount(enabled: boolean): CountQuery {
  return Records.useList(
    { limit: 1, q: encodeConditionQ(ANALYSED_OPEN_ALERTS) },
    { refetchInterval: REFRESH_MS, enabled },
  );
}

/**
 * The population that count is a share of: every alert still in play,
 * acknowledged and snoozed included.
 */
export function useOpenAlertCount(enabled: boolean): CountQuery {
  return Records.useList(
    { limit: 1, q: encodeConditionQ(OPEN_ALERTS) },
    { refetchInterval: REFRESH_MS, enabled },
  );
}

/**
 * The rows themselves: open alerts carrying an analysis, newest analysis first.
 *
 * The server order is NOT the order on screen — the view sorts client-side
 * (`sortRows`: most urgent by default, newest analysis on request). It still
 * matters, because it decides which rows survive the {@link ANALYSES_ROW_CAP}
 * ceiling on the one occasion it bites: the freshest analyses are the ones
 * worth keeping. The dotted path works on all three backends (SQLite
 * json_extract, Postgres jsonb path, Mongo native dotted sort); `orderby` is
 * passed through to the driver untouched. They disagree on where a MISSING
 * `analysis.at` sorts (Postgres puts NULLs first on DESC), which no longer
 * reaches the screen but can decide what a capped fetch keeps — see
 * `ANALYSED_OPEN_ALERTS` for why that is preferred to filtering those rows out.
 *
 * Only called from the view, which is only mounted on `?view=analyses`.
 */
export function useAnalysedOpenAlerts(): UseQueryResult<ListResponse<Record_>, ApiError> {
  return Records.useList(
    {
      limit: ANALYSES_ROW_CAP,
      q: encodeConditionQ(ANALYSED_OPEN_ALERTS),
      orderby: "agentic.analysis.at",
      asc: false,
    },
    { refetchInterval: REFRESH_MS },
  );
}
