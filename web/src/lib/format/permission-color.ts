import { isPlatformPermission } from "@/lib/auth/permissions";
import type { BadgeVariant } from "@/shared/ui/Badge";

// Hint at what a permission grants, by prefix:
//   rw_all / admin_*    → admin    (magenta) — full administrative power
//   rw_* / can_*        → warning  (gold)    — write / mutating access
//   ro_*                → info     (blue)    — read-only access
//   deny_* / anonymous  → muted    (gray)    — restricted / denied
//   ro_tenant/rw_tenant → warning  (gold)    — platform-tier; granted with care
//
// Shared by the Roles table and the Profile page so both render permissions
// with the same colour code, and so rw_* (gold) is always visually distinct
// from ro_* (blue).
//
// rw_all/admin_* moved off the severity red onto the privilege magenta for
// the same reason the `admin` role chip did: it names reach, not danger.
export function permissionBadgeVariant(p: string): BadgeVariant {
  if (isPlatformPermission(p)) return "warning";
  if (p === "rw_all" || p.startsWith("admin_")) return "admin";
  if (p.startsWith("rw_") || p.startsWith("can_")) return "warning";
  if (p.startsWith("ro_")) return "info";
  if (p.startsWith("deny_") || p === "anonymous") return "muted";
  return "neutral";
}
