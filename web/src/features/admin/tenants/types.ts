/** Mirrors the Tenant struct from internal/pluginimpl/tenant/plugin.go (Shared Contract §2). */
export type Tenant = {
  /** Immutable slug == tenant_id stamped on all scoped documents. */
  id: string;
  display_name: string;
  /** "active" | "suspended" */
  status: string;
  ingest_token?: string;
  created_at?: number;
  updated_at?: number;
};

/** One-time admin credential returned by tenant create / reset-admin. */
export type AdminCredential = {
  username: string;
  password: string;
  method: string;
  created: boolean;
};

/** Body for POST /tenant (create). */
export type CreateTenantBody = {
  id: string;
  display_name: string;
  status: string;
  /** default true on the server; false suppresses first-admin provisioning. */
  create_admin?: boolean;
  admin_username?: string;
};

/** Response from POST /tenant — the write result plus an optional one-time admin credential. */
export type CreateTenantResult = {
  added?: string[];
  admin?: AdminCredential;
};
