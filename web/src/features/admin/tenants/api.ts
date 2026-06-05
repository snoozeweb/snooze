import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
  type UseQueryResult,
} from "@tanstack/react-query";
import { api, type ApiError } from "@/lib/api/client";
import type { ListResponse } from "@/lib/api/resource";
import type { Tenant } from "./types";

// The tenant registry lives at /api/v1/tenant, gated on ro_tenant / rw_tenant.
// It is NOT a standard plugin resource (its collection is global, not tenant-
// scoped, and it is mounted on a separate router in the Go API), so we do NOT
// use defineResource() — instead we hand-write the hooks to match the exact
// path and semantics described in the Shared Contract §7.

const QUERY_KEY_ALL = ["tenant"] as const;
const QUERY_KEY_LIST = (params?: { offset?: number; limit?: number }) =>
  ["tenant", "list", JSON.stringify(params ?? {})] as const;
const QUERY_KEY_ONE = (id: string) => ["tenant", "one", id] as const;

export type TenantListParams = {
  offset?: number;
  limit?: number;
  orderby?: string;
  asc?: boolean;
};

export type TenantUpdateBody = {
  display_name?: string;
  status?: string;
  ingest_token?: string;
};

export const Tenants = {
  queryKey: {
    all: QUERY_KEY_ALL,
    list: QUERY_KEY_LIST,
    one: QUERY_KEY_ONE,
  },

  useList(
    params?: TenantListParams,
    options?: { refetchInterval?: number; enabled?: boolean },
  ): UseQueryResult<ListResponse<Tenant>, ApiError> {
    const query: Record<string, string | number | boolean | undefined> = {};
    if (params?.offset !== undefined) query["offset"] = params.offset;
    if (params?.limit !== undefined) query["limit"] = params.limit;
    if (params?.orderby !== undefined) query["orderby"] = params.orderby;
    if (params?.asc !== undefined) query["asc"] = params.asc;
    const q = Object.keys(query).length > 0 ? query : undefined;
    return useQuery<ListResponse<Tenant>, ApiError>({
      queryKey: QUERY_KEY_LIST(params),
      queryFn: ({ signal }) =>
        api<ListResponse<Tenant>>("GET", "/tenant", { ...(q ? { query: q } : {}), signal }),
      ...(options?.refetchInterval !== undefined
        ? { refetchInterval: options.refetchInterval }
        : {}),
      ...(options?.enabled !== undefined ? { enabled: options.enabled } : {}),
    });
  },

  useGet(id: string | undefined): UseQueryResult<Tenant, ApiError> {
    return useQuery<Tenant, ApiError>({
      queryKey: id ? QUERY_KEY_ONE(id) : ["disabled"],
      queryFn: ({ signal }) => api<Tenant>("GET", `/tenant/${id!}`, { signal }),
      enabled: !!id,
    });
  },

  useCreate(): UseMutationResult<Tenant, ApiError, Omit<Tenant, "created_at" | "updated_at">> {
    const qc = useQueryClient();
    return useMutation<Tenant, ApiError, Omit<Tenant, "created_at" | "updated_at">>({
      mutationFn: (body) => api<Tenant>("POST", "/tenant", { body }),
      onSuccess: () => {
        void qc.invalidateQueries({ queryKey: QUERY_KEY_ALL });
      },
    });
  },

  useUpdate(): UseMutationResult<Tenant, ApiError, { uid: string; body: TenantUpdateBody }> {
    const qc = useQueryClient();
    return useMutation<Tenant, ApiError, { uid: string; body: TenantUpdateBody }>({
      mutationFn: ({ uid, body }) => api<Tenant>("PATCH", `/tenant/${uid}`, { body }),
      onSuccess: (_data, vars) => {
        void qc.invalidateQueries({ queryKey: QUERY_KEY_ALL });
        void qc.invalidateQueries({ queryKey: QUERY_KEY_ONE(vars.uid) });
      },
    });
  },

  useRemove(): UseMutationResult<void, ApiError, string> {
    const qc = useQueryClient();
    return useMutation<void, ApiError, string>({
      mutationFn: (id) => api<void>("DELETE", `/tenant/${id}`),
      onSuccess: () => {
        void qc.invalidateQueries({ queryKey: QUERY_KEY_ALL });
      },
    });
  },
};
