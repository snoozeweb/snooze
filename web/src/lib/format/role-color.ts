// Map common role names to Badge variants so the UI hints at the
// responsibility level: admin/root → critical, oncall/sre → warning,
// analyst/triage → info, viewer/reader/auditor → muted.
// Unknown roles fall back to "neutral" so custom names still get a
// visually distinct chip.
import type { BadgeVariant } from "@/shared/ui/Badge";

const KEYWORDS: Array<[RegExp, BadgeVariant]> = [
  [/^(admin|root|owner|super)/i, "critical"],
  [/oncall|sre|operator|incident|page/i, "warning"],
  [/analyst|triage|sec(urity)?/i, "info"],
  [/viewer|reader|read[-_ ]?only|guest|auditor/i, "muted"],
  [/^(ldap|local|anonymous)$/i, "neutral"],
];

export function roleBadgeVariant(role: string): BadgeVariant {
  for (const [re, v] of KEYWORDS) {
    if (re.test(role)) return v;
  }
  return "neutral";
}
