import type { ReactNode } from "react";
import { useLocation } from "@tanstack/react-router";
import { useAuth } from "@/lib/auth/store";
import { AccessDenied } from "@/shared/auth/AccessDenied";
import { NAV_ITEMS } from "./nav-items";
import { visibleNavItems } from "./nav-list";

/**
 * RouteGuard gates the current route's content on exactly the same predicate
 * the nav uses to show/hide it (visibleNavItems — which honours platform-tier
 * permissions, not just any/all), rendering a clear AccessDenied state when the
 * user lacks access. Wrapping the shell's <Outlet/> in one place means a
 * deep-linked / bookmarked permission-gated page no longer loads a broken shell
 * or an ad-hoc, often-silent 403 — it shows a consistent, explained wall whose
 * escape CTA points at a page the user CAN see. Routes with no permission
 * requirement render unchanged.
 */
export function RouteGuard({ children }: { children: ReactNode }) {
  const { pathname } = useLocation();
  const { claims } = useAuth();
  const item = NAV_ITEMS.find((i) => i.to === pathname);
  // Only gate items that carry a permission requirement.
  if (!item?.permissions || item.permissions.length === 0) return <>{children}</>;
  const permitted = visibleNavItems(claims);
  if (permitted.some((i) => i.to === pathname)) return <>{children}</>;
  // Point the escape CTA at the user's first permitted destination so it's not
  // a dead-end back into another wall.
  const home = permitted[0];
  return (
    <AccessDenied
      perms={item.permissions}
      homeTo={home?.to ?? "/web/alerts"}
      homeLabel={home?.label ?? "Alerts"}
    />
  );
}
