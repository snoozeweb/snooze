import { Actions, Notifications } from "@/features/notifications/api";

/**
 * useConfigHealth reports whether the alert pipeline is configured — the count
 * of Actions and Notifications — so both the desktop HowToMenu and the mobile
 * MoreSheet can surface the same "No actions / No notifications" warning. A
 * count of `null` means "not loaded yet / not asked" (query disabled); `0`
 * means a real, warn-worthy misconfiguration.
 */
export function useConfigHealth(enabled: boolean): {
  actionCount: number | null;
  notifCount: number | null;
} {
  const actionList = Actions.useList({ limit: 1 }, { enabled });
  const notifList = Notifications.useList({ limit: 1 }, { enabled });
  return {
    actionCount: actionList.data?.meta.total ?? null,
    notifCount: notifList.data?.meta.total ?? null,
  };
}
