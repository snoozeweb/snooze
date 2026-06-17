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
