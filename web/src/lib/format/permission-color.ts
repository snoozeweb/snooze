import { isPlatformPermission } from "@/lib/auth/permissions";
import type { BadgeVariant } from "@/shared/ui/Badge";

// Hint at what a permission grants, by prefix:
//   rw_all / admin_*    → critical (red)   — full administrative power
//   rw_* / can_*        → warning  (amber) — write / mutating access
//   ro_*                → info     (blue)  — read-only access
//   deny_* / anonymous  → muted    (gray)  — restricted / denied
//   ro_tenant/rw_tenant → warning  (amber) — platform-tier; granted with care
//
// Shared by the Roles table and the Profile page so both render permissions
// with the same colour code, and so rw_* (amber) is always visually distinct
// from ro_* (blue).
export function permissionBadgeVariant(p: string): BadgeVariant {
  if (isPlatformPermission(p)) return "warning";
  if (p === "rw_all" || p.startsWith("admin_")) return "critical";
  if (p.startsWith("rw_") || p.startsWith("can_")) return "warning";
  if (p.startsWith("ro_")) return "info";
  if (p.startsWith("deny_") || p === "anonymous") return "muted";
  return "neutral";
}
