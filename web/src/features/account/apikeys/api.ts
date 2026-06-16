import { useMutation, useQuery, useQueryClient, type UseQueryResult } from "@tanstack/react-query";
import { api, type ApiError } from "@/lib/api/client";
import { defineResource } from "@/lib/api/resource";
import type { ApiKey, ApiKeyCreate, ApiKeyCreated } from "./types";

/** Admin surface: GET/PATCH/DELETE /api/v1/apikey (gated ro_apikey/rw_apikey). */
export const ApiKeys = defineResource<ApiKey>("apikey");

const MY_KEYS = ["apikeys", "me"] as const;

/** Self-service: the caller's own keys. */
export function useMyApiKeys(): UseQueryResult<ApiKey[], ApiError> {
  return useQuery<ApiKey[], ApiError>({
    queryKey: MY_KEYS,
    queryFn: async ({ signal }) => {
      const res = await api<{ data: ApiKey[] }>("GET", "/user/me/apikeys", { signal });
      return res.data ?? [];
    },
  });
}

export function useCreateMyApiKey() {
  const qc = useQueryClient();
  return useMutation<ApiKeyCreated, ApiError, ApiKeyCreate>({
    mutationFn: (body) => api<ApiKeyCreated>("POST", "/user/me/apikeys", { body }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: MY_KEYS }),
  });
}

export function useDeleteMyApiKey() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => api<void>("DELETE", `/user/me/apikeys/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: MY_KEYS }),
  });
}
