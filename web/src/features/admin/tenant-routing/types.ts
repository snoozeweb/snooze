/** Mirrors the TenantMatchRule struct from internal/pluginimpl/tenantmatch/plugin.go. */
export type TenantMatchRule = {
  uid?: string;
  match_type: "group" | "domain" | "login";
  match: string;
  tenant_id: string;
  priority?: number;
};

/** Body for POST /tenant_match (create). */
export type TenantMatchRuleBody = {
  match_type: "group" | "domain" | "login";
  match: string;
  tenant_id: string;
  priority?: number;
};

/** Body for PATCH /tenant_match/{uid} (partial update). */
export type TenantMatchRulePatch = {
  match_type?: "group" | "domain" | "login";
  match?: string;
  tenant_id?: string;
  priority?: number;
};
