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
