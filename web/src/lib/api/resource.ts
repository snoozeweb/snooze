import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
  type UseQueryResult,
} from "@tanstack/react-query";
import { api, type ApiError } from "./client";

export type SearchParams = {
  page?: number;
  pageSize?: number;
  sortBy?: string;
  sortOrder?: "asc" | "desc";
  search?: string;
  q?: string;
};

export type ListResponse<T> = {
  items: T[];
  total: number;
};

export type ResourceHooks<T, Create, Update> = {
  queryKey: {
    all: readonly [string];
    list: (params?: SearchParams) => readonly [string, "list", string];
    one: (uid: string) => readonly [string, "one", string];
  };
  useList: (
    params?: SearchParams,
    options?: { refetchInterval?: number },
  ) => UseQueryResult<ListResponse<T>, ApiError>;
  useGet: (uid: string | undefined) => UseQueryResult<T, ApiError>;
  useCreate: () => UseMutationResult<T, ApiError, Create>;
  useUpdate: () => UseMutationResult<T, ApiError, { uid: string; body: Update }>;
  useRemove: () => UseMutationResult<void, ApiError, string>;
};

function searchToQuery(
  params: SearchParams | undefined,
): Record<string, string | number | boolean | undefined> | undefined {
  if (!params) return undefined;
  const out: Record<string, string | number | boolean | undefined> = {};
  if (params.page !== undefined) out["page"] = params.page;
  if (params.pageSize !== undefined) out["pageSize"] = params.pageSize;
  if (params.sortBy !== undefined) out["sortBy"] = params.sortBy;
  if (params.sortOrder !== undefined) out["sortOrder"] = params.sortOrder;
  if (params.search !== undefined) out["search"] = params.search;
  if (params.q !== undefined) out["q"] = params.q;
  return Object.keys(out).length > 0 ? out : undefined;
}

export function defineResource<T, Create = Partial<T>, Update = Partial<T>>(
  plugin: string,
): ResourceHooks<T, Create, Update> {
  const keys = {
    all: [plugin] as const,
    list: (params?: SearchParams) => [plugin, "list", JSON.stringify(params ?? {})] as const,
    one: (uid: string) => [plugin, "one", uid] as const,
  };

  return {
    queryKey: keys,

    useList(params, options) {
      const query = searchToQuery(params);
      return useQuery<ListResponse<T>, ApiError>({
        queryKey: keys.list(params),
        queryFn: ({ signal }) =>
          api<ListResponse<T>>("GET", `/${plugin}`, {
            ...(query ? { query } : {}),
            signal,
          }),
        ...(options?.refetchInterval !== undefined
          ? { refetchInterval: options.refetchInterval }
          : {}),
      });
    },

    useGet(uid) {
      return useQuery<T, ApiError>({
        queryKey: uid ? keys.one(uid) : ["disabled"],
        queryFn: ({ signal }) => api<T>("GET", `/${plugin}/${uid!}`, { signal }),
        enabled: !!uid,
      });
    },

    useCreate() {
      const qc = useQueryClient();
      return useMutation<T, ApiError, Create>({
        mutationFn: (body) => api<T>("POST", `/${plugin}`, { body }),
        onSuccess: () => {
          void qc.invalidateQueries({ queryKey: keys.all });
        },
      });
    },

    useUpdate() {
      const qc = useQueryClient();
      return useMutation<T, ApiError, { uid: string; body: Update }>({
        mutationFn: ({ uid, body }) => api<T>("PATCH", `/${plugin}/${uid}`, { body }),
        onSuccess: (_data, vars) => {
          void qc.invalidateQueries({ queryKey: keys.all });
          void qc.invalidateQueries({ queryKey: keys.one(vars.uid) });
        },
      });
    },

    useRemove() {
      const qc = useQueryClient();
      return useMutation<void, ApiError, string>({
        mutationFn: (uid) => api<void>("DELETE", `/${plugin}/${uid}`),
        onSuccess: () => {
          void qc.invalidateQueries({ queryKey: keys.all });
        },
      });
    },
  };
}
