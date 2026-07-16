import { useMutation, useQuery, type UseQueryResult } from "@tanstack/react-query";
import { api, type ApiError } from "@/lib/api/client";

// Response shapes mirror internal/api/routes_housekeeping.go (and the
// generated OpenAPI operations runHousekeeping / housekeepingStatus). Both
// endpoints are gated by rw_all on the server.

/** Per-job outcome from a single housekeeping run. */
export type HousekeepingJobResult = {
  name?: string;
  error?: string;
  duration_ms?: number;
};

/** Body of POST /housekeeping/run. HTTP is 200 even when jobs failed. */
export type HousekeepingRunResult = {
  status?: string;
  jobs?: HousekeepingJobResult[];
  errors?: number;
};

/** Body of GET /housekeeping/status when the housekeeper is wired. */
export type HousekeepingStatus = {
  status?: string;
  registered_jobs?: number;
};

/**
 * useHousekeepingStatus probes whether the housekeeper is wired and how many
 * jobs are registered. The server returns 503 when rt.HK == nil, which lands
 * in `error` (ApiError.status === 503) so the panel can render a "not
 * configured" state rather than offering a Run button that would fail.
 *
 * retry:false keeps the 503 from being hammered, and there's nothing to poll —
 * the registered-job set is static for the process lifetime.
 */
export function useHousekeepingStatus(): UseQueryResult<HousekeepingStatus, ApiError> {
  return useQuery<HousekeepingStatus, ApiError>({
    queryKey: ["housekeeping", "status"],
    queryFn: ({ signal }) => api<HousekeepingStatus>("GET", "/housekeeping/status", { signal }),
    retry: false,
  });
}

/**
 * useHousekeepingRun fires every registered job synchronously on the server.
 * The mutation resolves with the per-job results even when individual jobs
 * failed; callers inspect `errors` to decide success vs partial-failure
 * messaging.
 */
export function useHousekeepingRun() {
  return useMutation<HousekeepingRunResult, ApiError, void>({
    mutationFn: () => api<HousekeepingRunResult>("POST", "/housekeeping/run"),
  });
}
