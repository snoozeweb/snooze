import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { api, type ApiError } from "@/lib/api/client";
import { defineResource, type ListResponse } from "@/lib/api/resource";
import { encodeConditionQ } from "@/lib/condition/serialize";

export type Comment = {
  uid?: string;
  record_uid: string;
  type: "comment" | "ack" | "close" | "open" | "esc" | "shelve" | "unshelve";
  message?: string;
  date_epoch?: number;
  user?: string;
};

export const Comments = defineResource<Comment>("comment");

export type RecordCommentsPage = {
  limit?: number;
  offset?: number;
};

export function useRecordComments(
  record_uid: string | undefined,
  page: RecordCommentsPage = {},
): UseQueryResult<ListResponse<Comment>, ApiError> {
  const q = record_uid
    ? encodeConditionQ({ type: "EQUALS", field: "record_uid", value: record_uid })
    : undefined;
  const limit = page.limit ?? 5;
  const offset = page.offset ?? 0;
  return useQuery<ListResponse<Comment>, ApiError>({
    queryKey: ["comment", "for-record", record_uid ?? "", limit, offset],
    queryFn: ({ signal }) =>
      api<ListResponse<Comment>>("GET", "/comment", {
        query: { ...(q !== undefined ? { q } : {}), orderby: "date_epoch", asc: true, limit, offset },
        signal,
      }),
    enabled: !!record_uid,
  });
}
