import type { IconName } from "@/shared/icons/icon-names";

export type NavGroup = "operate" | "configure" | "admin";

export type NavItem = {
  to: string;
  label: string;
  icon: IconName;
  group: NavGroup;
  permissions?: string[];
  shortcut?: string;
};

export const NAV_ITEMS: NavItem[] = [
  {
    to: "/web/alerts",
    label: "Alerts",
    icon: "file-text",
    group: "operate",
    permissions: ["ro_record", "rw_record"],
    shortcut: "mod+1",
  },
  {
    to: "/web/dashboard",
    label: "Dashboard",
    icon: "gauge",
    group: "operate",
    permissions: ["ro_stats", "rw_stats"],
    shortcut: "mod+2",
  },
  {
    to: "/web/snoozes",
    label: "Snoozes",
    icon: "bell-off",
    group: "operate",
    permissions: ["ro_snooze", "rw_snooze"],
    shortcut: "mod+3",
  },
  {
    to: "/web/rules",
    label: "Rules",
    icon: "scale",
    group: "configure",
    permissions: ["ro_rule", "rw_rule"],
    shortcut: "mod+4",
  },
  {
    to: "/web/notifications",
    label: "Notifications",
    icon: "megaphone",
    group: "configure",
    permissions: ["ro_notification", "rw_notification"],
    shortcut: "mod+5",
  },
  {
    to: "/web/admin/users",
    label: "Users",
    icon: "users",
    group: "admin",
    permissions: ["ro_user", "rw_user"],
  },
  {
    to: "/web/admin/roles",
    label: "Roles",
    icon: "user-plus",
    group: "admin",
    permissions: ["ro_role", "rw_role"],
  },
  {
    // "key" is not in ICON_NAMES; "lock" is the closest existing glyph.
    to: "/web/admin/apikeys",
    label: "API Keys",
    icon: "lock",
    group: "admin",
    permissions: ["ro_apikey", "rw_apikey"],
  },
  {
    to: "/web/admin/environments",
    label: "Environments",
    icon: "layers",
    group: "admin",
    permissions: ["ro_environment", "rw_environment"],
  },
  {
    to: "/web/admin/widgets",
    label: "Widgets",
    icon: "plug",
    group: "admin",
    permissions: ["ro_widget", "rw_widget"],
  },
  {
    to: "/web/admin/kv",
    label: "Key-values",
    icon: "book",
    group: "admin",
    permissions: ["ro_kv", "rw_kv"],
  },
  {
    to: "/web/admin/settings",
    label: "Settings",
    icon: "settings",
    group: "admin",
    permissions: ["ro_settings", "rw_settings"],
  },
  { to: "/web/admin/status", label: "Status", icon: "activity", group: "admin" },
  {
    to: "/web/admin/tenants",
    label: "Tenants",
    icon: "layers",
    group: "admin",
    permissions: ["ro_tenant", "rw_tenant"],
  },
];

export const GROUP_LABELS: Record<NavGroup, string> = {
  operate: "Operate",
  configure: "Configure",
  admin: "Admin",
};
