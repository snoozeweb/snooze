import { defineResource } from "@/lib/api/resource";
import type { TenantMatchRule, TenantMatchRuleBody, TenantMatchRulePatch } from "./types";

// The tenant_match collection lives at /api/v1/tenant_match, gated on
// ro_tenant / rw_tenant. It uses the standard defineResource pattern — unlike
// the Tenant registry, this is a normal plugin collection.
export const TenantMatchRules = defineResource<
  TenantMatchRule,
  TenantMatchRuleBody,
  TenantMatchRulePatch
>("tenant_match");
