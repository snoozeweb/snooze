import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { api, type ApiError } from "@/lib/api/client";
import type { ListResponse } from "@/lib/api/resource";
import { encodeConditionQ } from "@/lib/condition/serialize";
import type { AuditEntry } from "./types";

export type AuditPage = {
  limit?: number;
  offset?: number;
};

// useObjectAudit returns the audit-log entries for a single resource,
// newest last. The query is gated on objectId — calling it with undefined
// is fine (the hook stays disabled) so callers can mount conditionally
// in editor drawers that haven't loaded data yet.
export function useObjectAudit(
  objectType: string,
  objectId: string | undefined,
  page: AuditPage = {},
): UseQueryResult<ListResponse<AuditEntry>, ApiError> {
  const limit = page.limit ?? 5;
  const offset = page.offset ?? 0;
  const q =
    objectId !== undefined
      ? encodeConditionQ({
          type: "AND",
          args: [
            { type: "EQUALS", field: "object_type", value: objectType },
            { type: "EQUALS", field: "object_id", value: objectId },
          ],
        })
      : undefined;
  return useQuery<ListResponse<AuditEntry>, ApiError>({
    queryKey: ["audit", objectType, objectId ?? "", limit, offset],
    queryFn: ({ signal }) =>
      api<ListResponse<AuditEntry>>("GET", "/audit", {
        query: {
          ...(q !== undefined ? { q } : {}),
          orderby: "date_epoch",
          asc: true,
          limit,
          offset,
        },
        signal,
      }),
    enabled: !!objectId,
  });
}
