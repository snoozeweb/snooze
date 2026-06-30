import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { api, type ApiError } from "@/lib/api/client";
import type { ListResponse } from "@/lib/api/resource";
import { encodeConditionQ } from "@/lib/condition/serialize";
import type { Condition } from "@/lib/condition/types";
import type { AuditEntry } from "@/features/audit/types";

export type AuthAuditPage = {
  limit?: number;
  offset?: number;
  orderby?: string;
  asc?: boolean;
};

const AUTH_FILTER: Condition = { type: "EQUALS", field: "object_type", value: "auth" };

// useAuthAudit queries GET /api/v1/audit hard-pinned to object_type=auth.
// An optional extra Condition (from the search bar) is combined with AND.
export function useAuthAudit(
  filter: Condition | undefined,
  page: AuthAuditPage = {},
): UseQueryResult<ListResponse<AuditEntry>, ApiError> {
  const combined: Condition = filter ? { type: "AND", args: [AUTH_FILTER, filter] } : AUTH_FILTER;
  const q = encodeConditionQ(combined);
  const { limit = 50, offset = 0 } = page;

  return useQuery<ListResponse<AuditEntry>, ApiError>({
    queryKey: ["auth-audit", q, limit, offset, page.orderby, page.asc],
    queryFn: ({ signal }) =>
      api<ListResponse<AuditEntry>>("GET", "/audit", {
        query: {
          q,
          limit,
          offset,
          ...(page.orderby !== undefined ? { orderby: page.orderby } : {}),
          ...(page.asc !== undefined ? { asc: page.asc } : {}),
        },
        signal,
      }),
  });
}
