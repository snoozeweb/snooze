// TanStack Query hooks over /api/v1/record/{uid}/agentic — an alert's
// AI-authored analysis.
//
// The subtree is a *protected field*: the generic record endpoints refuse to
// write it, so this small dedicated route is the whole surface. It is not a
// CRUD collection, which is why `defineResource()` does not apply — there is
// no list, and the "one" is addressed by its parent record's uid.
import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
  type UseQueryResult,
} from "@tanstack/react-query";
import { api, ApiError } from "@/lib/api/client";
import type { components } from "@/lib/api/types.gen";
import { Records } from "../api";

export type AgenticEnvelope = components["schemas"]["AgenticEnvelope"];
export type AgenticRequest = components["schemas"]["AgenticRequest"];

/** Query-key namespace for everything under this route. */
export const ANALYSIS_QUERY_KEY = "agentic";

/** REST path (relative to /api/v1) of one record's analysis. */
export function analysisPath(uid: string): string {
  return `/record/${encodeURIComponent(uid)}/agentic`;
}

/** The key one alert's analysis is cached under. */
export function analysisQueryKey(uid: string | undefined): readonly [string, string] {
  return [ANALYSIS_QUERY_KEY, uid ?? ""];
}

export type AnalysisQueryOptions = {
  /**
   * Gate the request. Defaults to true. The hook additionally refuses to fire
   * without a uid, so a closed inspector or a row that has not loaded yet
   * costs nothing.
   */
  enabled?: boolean;
};

/**
 * useAnalysis reads one alert's analysis.
 *
 * **A 404 is not an error here.** The route answers 404 both for "no such
 * record" and for "this record carries no analysis", and the second is the
 * ordinary state of almost every alert — rendering it as a failure would put a
 * red box on every unanalysed alert anyone opens. Both collapse to `data ===
 * null`, which callers render as the empty state. A genuinely missing record
 * is already reported by the surface that owns the row.
 *
 * Every other status still rejects, so a 403 or a 500 reaches `error`.
 */
export function useAnalysis(
  uid: string | undefined,
  opts?: AnalysisQueryOptions,
): UseQueryResult<AgenticEnvelope | null, ApiError> {
  return useQuery<AgenticEnvelope | null, ApiError>({
    queryKey: analysisQueryKey(uid),
    queryFn: async ({ signal }) => {
      // Unreachable — `enabled` below already refuses a missing uid — but it
      // keeps the path builder honest without a cast.
      if (uid === undefined || uid === "") return null;
      try {
        return await api<AgenticEnvelope>("GET", analysisPath(uid), { signal });
      } catch (err) {
        if (err instanceof ApiError && err.status === 404) return null;
        throw err;
      }
    },
    enabled: (opts?.enabled ?? true) && uid !== undefined && uid !== "",
  });
}

export type SetAnalysisInput = {
  uid: string;
  body: AgenticRequest;
};

/**
 * invalidateAnalysis refreshes everything a write to the subtree can change:
 * the analysis itself, and the record lists — the alert table's severity cell
 * carries a confidence dot and the inspector header a cause line, both read
 * off the record document, so a write that only invalidated its own key would
 * leave the table showing yesterday's confidence until an unrelated refetch.
 */
function useInvalidateAnalysis(): (uid: string) => void {
  const qc = useQueryClient();
  return (uid: string) => {
    void qc.invalidateQueries({ queryKey: analysisQueryKey(uid) });
    void qc.invalidateQueries({ queryKey: Records.queryKey.all });
  };
}

/**
 * useSetAnalysis stores an analysis, replacing any previous one — the route is
 * a whole-subtree PUT, never a merge. The response carries the server-stamped
 * provenance, so the caller can show "by you, just now" without a re-read.
 *
 * A 422 arrives with per-field messages; run the error through
 * {@link analysisFieldErrors} to place them on the form.
 */
export function useSetAnalysis(): UseMutationResult<AgenticEnvelope, ApiError, SetAnalysisInput> {
  const invalidate = useInvalidateAnalysis();
  return useMutation<AgenticEnvelope, ApiError, SetAnalysisInput>({
    mutationFn: ({ uid, body }) => api<AgenticEnvelope>("PUT", analysisPath(uid), { body }),
    onSuccess: (_data, { uid }) => invalidate(uid),
  });
}

/**
 * useClearAnalysis removes an alert's analysis. Takes the record uid, mirroring
 * `Records.useRemove`.
 */
export function useClearAnalysis(): UseMutationResult<void, ApiError, string> {
  const invalidate = useInvalidateAnalysis();
  return useMutation<void, ApiError, string>({
    mutationFn: (uid) => api<void>("DELETE", analysisPath(uid)),
    onSuccess: (_data, uid) => invalidate(uid),
  });
}

/**
 * analysisFieldErrors extracts the per-field messages a 422 carries, keyed by
 * the server's JSON path (`root_cause.confidence`,
 * `remediation_plan.steps[0].risk`) — the same paths the local resolver in
 * `schema.ts` produces, so the two merge into one form error tree.
 *
 * It takes `unknown` rather than `ApiError` on purpose: a mutation's error is
 * whatever was thrown, and a caller should not have to prove it is an
 * `ApiError` before asking about fields. Anything else — a network `TypeError`,
 * a 403, a 422 with no `details` — is not a field problem and yields `{}`, so
 * the caller falls back to the one-line message.
 */
export function analysisFieldErrors(err: unknown): Record<string, string> {
  if (!(err instanceof ApiError) || err.details === undefined) return {};
  const out: Record<string, string> = {};
  for (const [path, message] of Object.entries(err.details)) {
    // The envelope's details map is free-form; only string values name a
    // failure a field can display.
    if (typeof message === "string") out[path] = message;
  }
  return out;
}
