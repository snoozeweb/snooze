// Map common role names to Badge variants so the UI hints at the
// responsibility level: platform_admin → platform (violet, the reserved
// super-role), admin/root → critical, oncall/sre → warning,
// analyst/triage → info, viewer/reader/auditor → muted.
// Unknown roles fall back to "neutral" so custom names still get a
// visually distinct chip.
import type { BadgeVariant } from "@/shared/ui/Badge";

// The reserved platform-tier super-role. Mirrors the backend's seeded
// "platform_admin" role (see internal/migrate). Held in one place so the
// roles table, users table, and any future surface colour it identically.
export const PLATFORM_ROLE = "platform_admin";

/** Returns true when role is the reserved platform_admin super-role. */
export function isPlatformRole(role: string): boolean {
  return role.toLowerCase() === PLATFORM_ROLE;
}

const KEYWORDS: Array<[RegExp, BadgeVariant]> = [
  [/^(admin|root|owner|super)/i, "critical"],
  [/oncall|sre|operator|incident|page/i, "warning"],
  [/analyst|triage|sec(urity)?/i, "info"],
  [/viewer|reader|read[-_ ]?only|guest|auditor/i, "muted"],
  [/^(ldap|local|anonymous)$/i, "neutral"],
];

export function roleBadgeVariant(role: string): BadgeVariant {
  if (isPlatformRole(role)) return "platform";
  for (const [re, v] of KEYWORDS) {
    if (re.test(role)) return v;
  }
  return "neutral";
}
