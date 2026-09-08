// Who may read the delivery log.
//
// `notificationlog` is a data-model plugin, so the server mints its RBAC pair
// from the collection name (`ro_notificationlog` / `rw_notificationlog`) and
// gates every route with it. A role scoped to `ro_notification` therefore sees
// the notifications page fine and gets a 403 on the Deliveries tab — which is
// why the UI hides that tab rather than rendering an error where the record
// used to be, and never fires the count probes at all (plan Risk 6).
import { useAuth } from "@/lib/auth/store";
import { hasAnyPermission } from "@/lib/auth/permissions";

/**
 * Any one of these grants read access. `ro_all` is the read-everything
 * wildcard and `rw_all` is already honoured inside `hasAnyPermission`, but
 * both are spelled out so the list reads the same as the nav items' own
 * `["ro_x", "rw_x"]` declarations.
 */
export const DELIVERY_PERMS = [
  "ro_notificationlog",
  "rw_notificationlog",
  "ro_all",
  "rw_all",
] as const;

/**
 * useCanReadDeliveries is the boolean behind every Deliveries surface. Callers
 * need the value (not just `<RequirePerm>`) because it also has to reach the
 * query hooks' `enabled` flag and the inspector's default-tab choice — a
 * hidden tab whose query still fires is a 403 probe on every row open.
 */
export function useCanReadDeliveries(): boolean {
  const { claims } = useAuth();
  return hasAnyPermission(claims, DELIVERY_PERMS);
}
