// TanStack Query hooks over the people directory and profile pictures:
// GET /api/v1/people, GET /api/v1/avatar/{method}/{name} and the caller's own
// PUT/DELETE /api/v1/user/me/avatar.
//
// Shared rather than feature-scoped because a person shows up everywhere — the
// alerts table's Owner column and owner filter, the comment timeline, the
// sidebar footer, the profile page — and every one of those must read the same
// cache so an upload repaints all of them at once.
import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
  type UseQueryResult,
} from "@tanstack/react-query";
import { useMemo } from "react";
import { api, ApiError } from "@/lib/api/client";
import type { components } from "@/lib/api/types.gen";

export type Person = components["schemas"]["Person"];
export type AvatarPayload = components["schemas"]["Avatar"];

/** The directory's cache key. */
export const PEOPLE_QUERY_KEY = ["people"] as const;
/** Prefix of every avatar image's cache key: `[prefix, method, name, version]`. */
export const AVATAR_QUERY_KEY = "avatar";

// Five minutes: the directory changes when an admin adds a user or someone
// uploads a picture, and the upload path invalidates it explicitly. Anything
// staler than this is a new colleague showing up as initials for a while.
const PEOPLE_STALE_MS = 5 * 60_000;

/** usePeople reads the enabled users of the caller's tenant. */
export function usePeople(opts?: { enabled?: boolean }): UseQueryResult<Person[], ApiError> {
  return useQuery<Person[], ApiError>({
    queryKey: PEOPLE_QUERY_KEY,
    queryFn: async ({ signal }) => {
      const res = await api<{ data?: Person[] }>("GET", "/people", { signal });
      return Array.isArray(res.data) ? res.data : [];
    },
    staleTime: PEOPLE_STALE_MS,
    enabled: opts?.enabled ?? true,
  });
}

/**
 * findPerson resolves a login (and, when known, its auth method) to its
 * directory entry. The method disambiguates the rare tenant with the same
 * login under two backends; without one, the first entry with that login
 * wins — the directory is sorted, so that is at least stable.
 */
export function findPerson(
  people: readonly Person[] | undefined,
  name: string,
  method?: string,
): Person | undefined {
  if (!people || name === "") return undefined;
  if (method) {
    const exact = people.find((p) => p.name === name && p.method === method);
    if (exact) return exact;
  }
  return people.find((p) => p.name === name);
}

/** usePerson is {@link findPerson} over the cached directory. */
export function usePerson(name: string, method?: string): Person | undefined {
  const { data } = usePeople({ enabled: name !== "" });
  return useMemo(() => findPerson(data, name, method), [data, name, method]);
}

/** The name to show for someone: their display name, else their login. */
export function personLabel(person: Person | undefined, name: string): string {
  const display = person?.display_name?.trim();
  return display ? display : name;
}

export function avatarQueryKey(
  method: string,
  name: string,
  version: string,
): readonly [string, string, string, string] {
  return [AVATAR_QUERY_KEY, method, name, version];
}

/**
 * useAvatarImage fetches someone's picture as a `data:` URL, or resolves to
 * null when they have none.
 *
 * Fetched through the API client rather than handed to `<img src>` because the
 * route needs the bearer token and an image request cannot carry one. Keyed by
 * the directory's `avatar_version` and never stale: a new upload is a new
 * version, so a cached picture is always the right one for its key. Nothing is
 * requested for a person without a version — the directory already said they
 * have no picture.
 */
export function useAvatarImage(
  method: string | undefined,
  name: string,
  version: string | undefined,
): UseQueryResult<string | null, ApiError> {
  const m = method ?? "";
  const v = version ?? "";
  return useQuery<string | null, ApiError>({
    queryKey: avatarQueryKey(m, name, v),
    queryFn: async ({ signal }) => {
      try {
        const res = await api<AvatarPayload>(
          "GET",
          `/avatar/${encodeURIComponent(m)}/${encodeURIComponent(name)}`,
          { signal },
        );
        return typeof res.data === "string" && res.data.startsWith("data:image/") ? res.data : null;
      } catch (err) {
        // Deleted between the directory read and this one: initials, not an
        // error state on every place the face appears.
        if (err instanceof ApiError && err.status === 404) return null;
        throw err;
      }
    },
    enabled: m !== "" && name !== "" && v !== "",
    staleTime: Infinity,
    retry: false,
  });
}

/** Refreshes every surface a picture change can alter. */
function useInvalidatePeople(): () => void {
  const qc = useQueryClient();
  return () => {
    void qc.invalidateQueries({ queryKey: PEOPLE_QUERY_KEY });
    void qc.invalidateQueries({ queryKey: [AVATAR_QUERY_KEY] });
  };
}

/** useUploadAvatar stores the caller's picture (a `data:image/png` URL). */
export function useUploadAvatar(): UseMutationResult<{ version: string }, ApiError, string> {
  const invalidate = useInvalidatePeople();
  return useMutation<{ version: string }, ApiError, string>({
    mutationFn: (data) => api<{ version: string }>("PUT", "/user/me/avatar", { body: { data } }),
    onSuccess: invalidate,
  });
}

/** useRemoveAvatar drops the caller's picture; they go back to initials. */
export function useRemoveAvatar(): UseMutationResult<void, ApiError, void> {
  const invalidate = useInvalidatePeople();
  return useMutation<void, ApiError, void>({
    mutationFn: () => api<void>("DELETE", "/user/me/avatar"),
    onSuccess: invalidate,
  });
}
