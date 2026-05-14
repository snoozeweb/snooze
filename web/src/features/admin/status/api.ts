import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { api, type ApiError } from "@/lib/api/client";
import type { ClusterStatus } from "./types";

export function useClusterStatus(): UseQueryResult<ClusterStatus, ApiError> {
  return useQuery<ClusterStatus, ApiError>({
    queryKey: ["cluster-status"],
    queryFn: ({ signal }) => api<ClusterStatus>("GET", "/cluster/status", { signal }),
    refetchInterval: 15_000,
    retry: false,
  });
}
