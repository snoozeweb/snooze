import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { api, type ApiError } from "@/lib/api/client";
import { Comments, type Comment } from "@/features/alerts/comments";
import type { ListResponse } from "@/lib/api/resource";
import { encodeConditionQ } from "@/lib/condition/serialize";
import type { StatsResponse } from "./types";

/** Recent *attributed* user actions across all alerts — the dashboard activity
 * feed. System/auto comments (escalations, auto-close) carry no `user`, so an
 * `EXISTS user` filter drops them server-side. */
export function useRecentActivity(limit = 15): UseQueryResult<ListResponse<Comment>, ApiError> {
  return Comments.useList(
    {
      q: encodeConditionQ({ type: "EXISTS", field: "user" }),
      orderby: "date_epoch",
      asc: false,
      limit,
    },
    { refetchInterval: 30_000 },
  );
}

export type StatsParams = {
  from: string;
  to: string;
  bucket: number;
};

export type UseStatsOptions = {
  /** Defaults to true; pass false (e.g. empty bounds) to keep the query idle. */
  enabled?: boolean;
};

export function useStats(
  params: StatsParams,
  options: UseStatsOptions = {},
): UseQueryResult<StatsResponse, ApiError> {
  // Bounds must be present for /stats to mean anything; an explicit
  // enabled:false (or a missing from/to) keeps the prior-window query idle.
  const enabled = (options.enabled ?? true) && params.from !== "" && params.to !== "";
  return useQuery<StatsResponse, ApiError>({
    queryKey: ["stats", params.from, params.to, params.bucket],
    queryFn: ({ signal }) =>
      api<StatsResponse>("GET", "/stats", {
        query: { from: params.from, to: params.to, bucket: params.bucket },
        signal,
      }),
    refetchInterval: 30_000,
    enabled,
  });
}
