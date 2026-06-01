import { useMutation, type UseMutationResult } from "@tanstack/react-query";
import { api, type ApiError } from "@/lib/api/client";
import { defineResource } from "@/lib/api/resource";
import type { Action, Notification } from "./types";

export const Notifications = defineResource<Notification>("notification");
export const Actions = defineResource<Action>("action");

export type ActionTestRequest = {
  selected: string;
  subcontent: Record<string, unknown>;
};

export type ActionTestResult = { ok: boolean };

// useTestAction delivers one synthetic alert through the named notifier using
// the (possibly unsaved) action config. Powers the "Send test" button.
export function useTestAction(): UseMutationResult<
  ActionTestResult,
  ApiError,
  ActionTestRequest
> {
  return useMutation<ActionTestResult, ApiError, ActionTestRequest>({
    mutationFn: (body) => api<ActionTestResult>("POST", "/action/test", { body }),
  });
}
