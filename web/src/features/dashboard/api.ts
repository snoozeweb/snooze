import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { api, type ApiError } from "@/lib/api/client";
import type { StatsResponse } from "./types";

export type StatsParams = {
  from: string;
  to: string;
  bucket: number;
};

export function useStats(params: StatsParams): UseQueryResult<StatsResponse, ApiError> {
  return useQuery<StatsResponse, ApiError>({
    queryKey: ["stats", params.from, params.to, params.bucket],
    queryFn: ({ signal }) =>
      api<StatsResponse>("GET", "/stats", {
        query: { from: params.from, to: params.to, bucket: params.bucket },
        signal,
      }),
    refetchInterval: 30_000,
  });
}
