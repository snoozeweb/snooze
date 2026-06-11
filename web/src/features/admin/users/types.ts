export type User = {
  uid?: string;
  name: string;
  // The auth backend that owns this user record. "method" is the canonical
  // backend field (auth.Claims.Method); "type" is its legacy alias. Values are
  // "local"/"ldap" plus any configured OIDC method (e.g. "microsoft").
  type?: string;
  method?: string;
  roles?: string[];
  // Groups assigned by the auth backend (LDAP MemberAttribute, local doc field,
  // or OIDC roles/groups claims). Populated at login and used for RBAC group →
  // role resolution. Read-only on the wire from the UI's perspective.
  groups?: string[];
  // Whether the account may log in. Absent is treated as enabled; setting it to
  // false blocks login + token refresh (see internal/auth + routes_login.go).
  enabled?: boolean;
  // Epoch seconds of the most recent successful login. Written by
  // internal/api/routes_login.go updateLastLogin()/provisionOIDCUser().
  last_login?: number;
  // Epoch seconds the record was first created (set on JIT/first login).
  created_at?: number;
  comment?: string;
  /** Only sent on create. */
  password?: string;
};
