import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { api, type ApiError } from "@/lib/api/client";
import type { InputsResponse } from "./types";

export function useInputActivity(): UseQueryResult<InputsResponse, ApiError> {
  return useQuery<InputsResponse, ApiError>({
    queryKey: ["input-activity"],
    queryFn: ({ signal }) => api<InputsResponse>("GET", "/inputs", { signal }),
    refetchInterval: 30_000,
    retry: false,
  });
}
