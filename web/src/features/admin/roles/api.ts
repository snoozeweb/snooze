import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { api, type ApiError } from "@/lib/api/client";
import { defineResource } from "@/lib/api/resource";
import type { Role } from "./types";

export const Roles = defineResource<Role>("role");

// usePermissionsCatalogue fetches the union of permission strings advertised
// by every registered backend plugin. Source of truth: the Go-side handler at
// internal/api/routes_schema.go `handlePermissions`, mounted as
// `GET /api/v1/permissions`. The shape is `{ data: string[] }`.
export function usePermissionsCatalogue(): UseQueryResult<string[], ApiError> {
  return useQuery<string[], ApiError>({
    queryKey: ["permissions", "list"],
    queryFn: async ({ signal }) => {
      const res = await api<{ data: string[] }>("GET", "/permissions", { signal });
      return res.data ?? [];
    },
    // The catalogue is essentially static for the lifetime of a session;
    // no need to refetch on focus.
    staleTime: 60_000,
  });
}
