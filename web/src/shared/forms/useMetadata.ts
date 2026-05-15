import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { api, type ApiError } from "@/lib/api/client";
import type { Metadata } from "./types";

type Envelope<T> = { data: T };

const KEY_ALL = ["metadata"] as const;
const KEY_ONE = (name: string) => ["metadata", name] as const;

const STALE = 5 * 60_000;

export function useAllMetadata(): UseQueryResult<Metadata[], ApiError> {
  return useQuery<Metadata[], ApiError>({
    queryKey: KEY_ALL,
    queryFn: async ({ signal }) => {
      const env = await api<Envelope<Metadata[]>>("GET", `/metadata`, { signal });
      return env.data;
    },
    staleTime: STALE,
  });
}

export function usePluginMetadata(
  name: string | undefined,
): UseQueryResult<Metadata, ApiError> {
  return useQuery<Metadata, ApiError>({
    queryKey: name ? KEY_ONE(name) : ["metadata", "__disabled__"],
    queryFn: async ({ signal }) => {
      const env = await api<Envelope<Metadata>>("GET", `/metadata/${name!}`, { signal });
      return env.data;
    },
    enabled: !!name,
    staleTime: STALE,
  });
}
