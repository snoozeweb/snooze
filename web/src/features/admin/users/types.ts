export type User = {
  uid?: string;
  name: string;
  // The auth backend that owns this user record. "method" is the canonical
  // backend field (auth.Claims.Method); "type" is its legacy alias.
  type?: "local" | "ldap";
  method?: "local" | "ldap";
  roles?: string[];
  // Groups assigned by the auth backend (LDAP MemberAttribute or local
  // doc field). Populated at login and used for RBAC group → role
  // resolution. Read-only on the wire from the UI's perspective.
  groups?: string[];
  // Epoch seconds of the most recent successful login. Written by
  // internal/api/routes_login.go updateLastLogin().
  last_login?: number;
  comment?: string;
  /** Only sent on create. */
  password?: string;
};
