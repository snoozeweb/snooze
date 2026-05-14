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

export function useRecordComments(
  record_uid: string | undefined,
): UseQueryResult<ListResponse<Comment>, ApiError> {
  const q = record_uid
    ? encodeConditionQ({ type: "EQUALS", field: "record_uid", value: record_uid })
    : undefined;
  return useQuery<ListResponse<Comment>, ApiError>({
    queryKey: ["comment", "for-record", record_uid ?? ""],
    queryFn: ({ signal }) =>
      api<ListResponse<Comment>>("GET", "/comment", {
        query: { ...(q !== undefined ? { q } : {}), orderby: "date_epoch", asc: true, limit: 100 },
        signal,
      }),
    enabled: !!record_uid,
  });
}
