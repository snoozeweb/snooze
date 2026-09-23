import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
  type UseQueryResult,
} from "@tanstack/react-query";
import { api, type ApiError } from "@/lib/api/client";
import { defineResource } from "@/lib/api/resource";
import type { Record_ } from "./types";
import { encodeConditionQ, type Condition } from "@/lib/condition/serialize";
import { ACTIVE_ALERTS } from "./tabs";
import { Comments } from "./comments";

export const Records = defineResource<Record_>("record");

/**
 * Returns the total count of active alerts (not ack'd, not closed, not snoozed).
 * Polls every 30 s when enabled. Consumers read `data?.meta.total`.
 */
export function useActiveAlertCount(enabled: boolean) {
  return Records.useList(
    { limit: 1, q: encodeConditionQ(ACTIVE_ALERTS) },
    { refetchInterval: 30_000, enabled },
  );
}

export type CommentInput = {
  record_uid: string;
  type: "ack" | "close" | "open" | "esc" | "comment" | "shelve" | "unshelve" | "assign" | "release";
  message?: string;
  /** seconds; only meaningful when type=="shelve". 0 → use server default. */
  duration?: number;
  /** Login of the new owner; required when type=="assign". */
  assignee?: string;
  /** The assignee's auth method, when the picker knows it (always, from the
   *  people directory) — saves the server a lookup and an ambiguity. */
  assignee_method?: string;
};

export function useCommentRecord(): UseMutationResult<unknown, ApiError, CommentInput> {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CommentInput) => api<unknown>("POST", "/comment", { body: input }),
    onSuccess: () => {
      // A state-changing comment (ack/close/open/esc) mutates the record's state
      // server-side AND appends to the comment log, so both must refetch: the
      // record list/count for the new state, and any mounted comment timeline
      // for the new entry. Invalidating only one leaves the other stale — the
      // bug that let a timeline-composer ack silently desync the alert list.
      // Ack/close/assign/release also move ownership; the owner filter's
      // counts live under the same `record` prefix (see useRecordOwners), so
      // this one call refreshes them too.
      void qc.invalidateQueries({ queryKey: Records.queryKey.all });
      void qc.invalidateQueries({ queryKey: Comments.queryKey.all });
    },
  });
}

// Default TTL applied when unshelving a record that was shelved with no
// prior magnitude (e.g. an alert that arrived before the server stamped a
// default TTL, or one shelved through an early-rewrite call that stored
// `ttl=-1`). Matches the file-config default in
// internal/config/schema/housekeeper.go::DefaultHousekeeper (48h).
const FALLBACK_UNSHELVE_TTL = 48 * 60 * 60;

/** @deprecated — permanent-exempt only; timed shelve uses useCommentRecord */
export type ShelveInput = {
  uid: string;
  /** Whether we're shelving (true) or unshelving (false). */
  shelve: boolean;
  /**
   * Current TTL on the record so the toggle can preserve the original
   * magnitude — shelve flips the sign negative, unshelve flips it back
   * positive. Mirrors the old Vue toggle_ttl helper. Undefined or zero
   * means "no magnitude to preserve" and we fall back to a sensible
   * default; the server's stampDefaultTTL hook fills in fresh records'
   * TTL at ingest, so this fallback only triggers for pre-stamp legacy
   * rows.
   */
  currentTTL?: number | undefined;
};

/** @deprecated — permanent-exempt only; timed shelve uses useCommentRecord */
export function useShelveRecord(): UseMutationResult<unknown, ApiError, ShelveInput> {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ uid, shelve, currentTTL }) => {
      const nextTTL = computeNextTTL(shelve, currentTTL);
      return api<unknown>("PATCH", `/record/${uid}`, { body: { ttl: nextTTL } });
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: Records.queryKey.all });
    },
  });
}

// ── Bulk operations ───────────────────────────────────────────────────────────

/**
 * encodeUidsAsQ wraps a uid list into an IN condition and base64url-encodes it
 * for use as the `?q=` parameter of bulk endpoints.
 * Produces: { type:"IN", field:"uid", value:[...uids] }
 */
export function encodeUidsAsQ(uids: string[]): string {
  const cond: Condition = { type: "IN", field: "uid", value: uids };
  return encodeConditionQ(cond);
}

export type BulkStateInput = {
  q?: string;
  state: "ack" | "close" | "open" | "esc";
  message?: string;
};

export type BulkStateResponse = {
  matched: number;
  updated: number;
  state: string;
};

export function useBulkStateRecord(): UseMutationResult<
  BulkStateResponse,
  ApiError,
  BulkStateInput
> {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ q, state, message }) =>
      api<BulkStateResponse>("POST", "/record/bulk_state", {
        ...(q ? { query: { q } } : {}),
        body: { state, ...(message ? { message } : {}) },
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: Records.queryKey.all });
    },
  });
}

export type BulkOwnerInput = {
  q?: string;
  action: "assign" | "release";
  /** Required for `assign`. */
  assignee?: string;
  assignee_method?: string;
  message?: string;
};

export type BulkOwnerResponse = {
  matched: number;
  updated: number;
  action: string;
};

/**
 * useBulkOwnerRecord assigns or releases every record matching `q` in one
 * call — the ownership twin of useBulkStateRecord, and like it, it writes no
 * per-record comment (the message goes to the audit summary).
 */
export function useBulkOwnerRecord(): UseMutationResult<
  BulkOwnerResponse,
  ApiError,
  BulkOwnerInput
> {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ q, action, assignee, assignee_method, message }) =>
      api<BulkOwnerResponse>("POST", "/record/bulk_owner", {
        ...(q ? { query: { q } } : {}),
        body: {
          action,
          ...(assignee ? { assignee } : {}),
          ...(assignee_method ? { assignee_method } : {}),
          ...(message ? { message } : {}),
        },
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: Records.queryKey.all });
    },
  });
}

export type OwnerCount = { owner: string; count: number };
export type OwnerCounts = { data: OwnerCount[]; unowned: number; total: number };

/**
 * The owner filter's counts: how many records matching `q` each owner has,
 * plus the unowned bucket. Keyed under the `record` prefix on purpose, so
 * every mutation that invalidates the record lists (ack, close, assign,
 * release, bulk_state, bulk_owner…) refreshes these counts with the same
 * call instead of having to remember a second key.
 */
export function useRecordOwners(
  q: string | undefined,
  options?: { refetchInterval?: number; enabled?: boolean },
): UseQueryResult<OwnerCounts, ApiError> {
  return useQuery<OwnerCounts, ApiError>({
    queryKey: [...Records.queryKey.all, "owners", q ?? ""],
    queryFn: async ({ signal }) => {
      const res = await api<Partial<OwnerCounts>>("GET", "/record/owners", {
        ...(q ? { query: { q } } : {}),
        signal,
      });
      return {
        data: Array.isArray(res.data) ? res.data : [],
        unowned: res.unowned ?? 0,
        total: res.total ?? 0,
      };
    },
    // Chips shouldn't blank out while a new tab's counts load.
    placeholderData: keepPreviousData,
    ...(options?.refetchInterval !== undefined ? { refetchInterval: options.refetchInterval } : {}),
    enabled: options?.enabled ?? true,
  });
}

export type BulkUpdateInput = {
  q?: string;
  set?: Record<string, unknown>;
  tag?: string[];
  untag?: string[];
};

export type BulkUpdateResponse = {
  matched: number;
  set: number;
  tagged: number;
  untagged: number;
};

export function useBulkUpdateRecord(): UseMutationResult<
  BulkUpdateResponse,
  ApiError,
  BulkUpdateInput
> {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ q, set, tag, untag }) =>
      api<BulkUpdateResponse>("POST", "/record/bulk_update", {
        ...(q ? { query: { q } } : {}),
        body: {
          ...(set ? { set } : {}),
          ...(tag ? { tag } : {}),
          ...(untag ? { untag } : {}),
        },
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: Records.queryKey.all });
    },
  });
}

/** @deprecated — permanent-exempt only; timed shelve uses useCommentRecord */
// computeNextTTL emits the new ttl for a shelve / unshelve toggle. The
// rules mirror Snooze 1.x's web/src/views/Record.vue::toggle_ttl, with one
// fix: that helper multiplied by -1 unconditionally, which silently
// no-op'd records that already had ttl=0. We treat ttl<=0 as
// "no magnitude" and apply the fallback default instead.
function computeNextTTL(shelve: boolean, current: number | undefined): number {
  if (shelve) {
    // Shelve: drop into negative space. Preserve magnitude if we have one,
    // otherwise stamp -1 so the row at least matches `ttl < 0`.
    if (current !== undefined && current > 0) return -current;
    if (current !== undefined && current < 0) return current; // already shelved
    return -1;
  }
  // Unshelve: restore the magnitude if we had one, otherwise the default.
  if (current !== undefined && current < 0) return -current;
  if (current !== undefined && current > 0) return current; // already unshelved
  return FALLBACK_UNSHELVE_TTL;
}
