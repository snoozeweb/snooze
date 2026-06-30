import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { defineResource } from "@/lib/api/resource";
import type { ListResponse, SearchParams } from "@/lib/api/resource";
import { api, type ApiError } from "@/lib/api/client";
import type { Heartbeat } from "./types";
import type { HeartbeatStatus } from "./types";

export const Heartbeats = defineResource<Heartbeat>("heartbeat");

export type HeartbeatListParams = SearchParams & {
  status?: HeartbeatStatus;
};

/**
 * useHeartbeatList — extends Heartbeats.useList with the backend-specific
 * `?status=` filter that defineResource's searchToQuery does not pass through
 * (it only knows the standard SearchParams keys).
 */
export function useHeartbeatList(
  params?: HeartbeatListParams,
  options?: { refetchInterval?: number; enabled?: boolean },
) {
  return useQuery<ListResponse<Heartbeat>, ApiError>({
    queryKey: ["heartbeat", "list", JSON.stringify(params ?? {})],
    queryFn: ({ signal }) => {
      const query: Record<string, string | number | boolean | undefined> = {};
      if (params?.offset !== undefined) query["offset"] = params.offset;
      if (params?.limit !== undefined) query["limit"] = params.limit;
      if (params?.orderby !== undefined) query["orderby"] = params.orderby;
      if (params?.asc !== undefined) query["asc"] = params.asc;
      if (params?.search !== undefined) query["search"] = params.search;
      if (params?.q !== undefined) query["q"] = params.q;
      if (params?.status !== undefined) query["status"] = params.status;
      return api<ListResponse<Heartbeat>>("GET", "/heartbeat", {
        ...(Object.keys(query).length > 0 ? { query } : {}),
        signal,
      });
    },
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval !== undefined ? { refetchInterval: options.refetchInterval } : {}),
    ...(options?.enabled !== undefined ? { enabled: options.enabled } : {}),
  });
}
