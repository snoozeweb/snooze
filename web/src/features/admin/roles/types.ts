export type Role = {
  uid?: string;
  name: string;
  permissions?: string[];
  // Auth-backend groups (LDAP group CNs, OIDC roles/groups claim values such as
  // "GrafanaAdmin") that auto-grant this role on login. This is the group→role
  // mapping. Matched against the identity's groups by the RBAC resolver.
  groups?: string[];
  comment?: string;
};
