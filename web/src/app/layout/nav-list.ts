import type { JwtClaims } from "@/lib/auth/jwt";
import {
  hasAnyPermission,
  hasPlatformPermission,
  isPlatformPermission,
} from "@/lib/auth/permissions";
import { NAV_ITEMS, type NavItem } from "./nav-items";

// Mirrors the backend's RequirePlatformPerm for platform-tier items (e.g.
// Tenants): a literal platform perm AND default-tenant origin — rw_all does
// not count. Other items use hasAnyPermission. Identical to the Sidebar's
// historical inline filter; centralized so all three nav surfaces agree.
export function visibleNavItems(claims: JwtClaims | null): NavItem[] {
  return NAV_ITEMS.filter((i) => {
    if (!i.permissions || i.permissions.length === 0) return true;
    if (i.permissions.some(isPlatformPermission)) {
      return hasPlatformPermission(claims, i.permissions);
    }
    return hasAnyPermission(claims, i.permissions);
  });
}

/**
 * firstLandingPath returns the first nav destination the user is actually
 * allowed to see, used as the post-login / root-redirect landing target so a
 * user without alert permissions isn't dumped onto the record-gated Alerts page
 * (an Access-denied wall). Falls back to /web/alerts when nothing is visible.
 */
export function firstLandingPath(claims: JwtClaims | null): string {
  return visibleNavItems(claims)[0]?.to ?? "/web/alerts";
}
