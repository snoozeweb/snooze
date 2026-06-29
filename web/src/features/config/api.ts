import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { api, type ApiError } from "@/lib/api/client";
import { CONSOLE_FALLBACK, type ConsoleConfig } from "./types";

/** TanStack Query key for the org-wide console config document. */
export const consoleConfigKey = ["config", "console"] as const;

type ConsoleConfigEnvelope = { data: ConsoleConfig };

/**
 * useConsoleConfig fetches the org-wide web-console defaults from the public
 * GET /api/v1/config endpoint. The data is org-static, so it is cached
 * aggressively (no auto-refetch) and shared across the app via the
 * `consoleConfigKey` query key.
 *
 * `placeholderData` is the current hardcodes (CONSOLE_FALLBACK) so callers
 * always read a usable config — before the fetch resolves AND when it fails.
 * The query never retries: a missing config blob is non-fatal, the fallback
 * is the intended behaviour.
 */
export function useConsoleConfig(): UseQueryResult<ConsoleConfig, ApiError> {
  return useQuery<ConsoleConfig, ApiError>({
    queryKey: consoleConfigKey,
    queryFn: ({ signal }) =>
      api<ConsoleConfigEnvelope>("GET", "/config", { signal }).then((env) => env.data),
    // Org-wide defaults rarely change; don't poll. A page reload re-fetches.
    staleTime: Infinity,
    gcTime: Infinity,
    retry: false,
    placeholderData: CONSOLE_FALLBACK,
  });
}

/**
 * fetchConsoleConfig fetches the console config imperatively (no React), for
 * boot-time wiring (main.tsx) that primes the severity-rank ladder before the
 * SPA renders. Returns the server document, or the fallback on any error so
 * the caller never has to branch.
 */
export async function fetchConsoleConfig(): Promise<ConsoleConfig> {
  try {
    const env = await api<ConsoleConfigEnvelope>("GET", "/config");
    return env.data;
  } catch {
    return CONSOLE_FALLBACK;
  }
}
