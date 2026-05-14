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

export function useShelveRecord(): UseMutationResult<
  unknown,
  ApiError,
  { uid: string; shelve: boolean }
> {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ uid, shelve }) =>
      api<unknown>("PATCH", `/record/${uid}`, { body: { ttl: shelve ? -1 : 0 } }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: Records.queryKey.all });
    },
  });
}
