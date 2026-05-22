import { useMutation, useQueryClient, type UseMutationResult } from "@tanstack/react-query";
import { api, type ApiError } from "@/lib/api/client";
import { defineResource } from "@/lib/api/resource";
import type { Record_ } from "./types";

export const Records = defineResource<Record_>("record");

export type CommentInput = {
  record_uid: string;
  type: "ack" | "close" | "open" | "esc" | "comment";
  message?: string;
};

export function useCommentRecord(): UseMutationResult<unknown, ApiError, CommentInput> {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CommentInput) => api<unknown>("POST", "/comment", { body: input }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: Records.queryKey.all });
    },
  });
}

// Default TTL applied when unshelving a record that was shelved with no
// prior magnitude (e.g. an alert that arrived before the server stamped a
// default TTL, or one shelved through an early-rewrite call that stored
// `ttl=-1`). Matches the file-config default in
// internal/config/schema/housekeeper.go::DefaultHousekeeper (48h).
const FALLBACK_UNSHELVE_TTL = 48 * 60 * 60;

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
