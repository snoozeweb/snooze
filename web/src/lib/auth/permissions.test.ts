import { describe, expect, it } from "vitest";
import {
  hasAllPermissions,
  hasAnyPermission,
  hasPermission,
  hasPlatformPermission,
  isPlatformPermission,
} from "./permissions";
import type { JwtClaims } from "./jwt";

const adminClaims: JwtClaims = { sub: "admin", permissions: ["rw_rule", "rw_record", "ro_user"] };
const readOnlyClaims: JwtClaims = { sub: "ro", permissions: ["ro_rule", "ro_record"] };
const noPermsClaims: JwtClaims = { sub: "weird" };
const nullClaims = null;

describe("hasPermission", () => {
  it("returns true when the claim is in the list", () => {
    expect(hasPermission(adminClaims, "rw_rule")).toBe(true);
  });
  it("returns false when the claim is missing", () => {
    expect(hasPermission(readOnlyClaims, "rw_rule")).toBe(false);
  });
  it("returns false on null claims", () => {
    expect(hasPermission(nullClaims, "rw_rule")).toBe(false);
  });
  it("returns false when permissions array is absent", () => {
    expect(hasPermission(noPermsClaims, "rw_rule")).toBe(false);
  });
});

describe("hasAnyPermission", () => {
  it("returns true when any one matches", () => {
    expect(hasAnyPermission(readOnlyClaims, ["rw_rule", "ro_rule"])).toBe(true);
  });
  it("returns false when none match", () => {
    expect(hasAnyPermission(readOnlyClaims, ["rw_user", "rw_settings"])).toBe(false);
  });
  it("returns false on null claims", () => {
    expect(hasAnyPermission(nullClaims, ["rw_rule"])).toBe(false);
  });
  it("returns false on empty list", () => {
    expect(hasAnyPermission(adminClaims, [])).toBe(false);
  });
});

describe("hasAllPermissions", () => {
  it("returns true when every claim is present", () => {
    expect(hasAllPermissions(adminClaims, ["rw_rule", "rw_record"])).toBe(true);
  });
  it("returns false when one is missing", () => {
    expect(hasAllPermissions(adminClaims, ["rw_rule", "rw_user"])).toBe(false);
  });
  it("returns true on empty list (vacuous)", () => {
    expect(hasAllPermissions(adminClaims, [])).toBe(true);
  });
});

describe("isPlatformPermission", () => {
  it("returns true for ro_tenant", () => {
    expect(isPlatformPermission("ro_tenant")).toBe(true);
  });
  it("returns true for rw_tenant", () => {
    expect(isPlatformPermission("rw_tenant")).toBe(true);
  });
  it("returns false for ro_record", () => {
    expect(isPlatformPermission("ro_record")).toBe(false);
  });
  it("returns false for rw_all", () => {
    expect(isPlatformPermission("rw_all")).toBe(false);
  });
});

// hasPlatformPermission mirrors the backend's RequirePlatformPerm
// (internal/api/middleware/permission.go): literal perm membership (rw_all does
// NOT satisfy it) AND default-tenant origin.
describe("hasPlatformPermission", () => {
  const platformPerms = ["ro_tenant", "rw_tenant"];
  const defaultAdmin: JwtClaims = {
    sub: "root",
    tenant_id: "default",
    permissions: ["ro_tenant", "rw_tenant"],
  };
  const wildcardOnly: JwtClaims = { sub: "admin", tenant_id: "default", permissions: ["rw_all"] };
  const otherTenant: JwtClaims = { sub: "alice", tenant_id: "acme", permissions: ["rw_tenant"] };
  const legacyNoTenant: JwtClaims = { sub: "legacy", permissions: ["ro_tenant"] };

  it("returns true for a default-tenant user holding a literal platform perm", () => {
    expect(hasPlatformPermission(defaultAdmin, platformPerms)).toBe(true);
  });
  it("returns false for rw_all without a literal platform perm (wildcard is not honored)", () => {
    expect(hasPlatformPermission(wildcardOnly, platformPerms)).toBe(false);
  });
  it("returns false for a non-default tenant even with rw_tenant", () => {
    expect(hasPlatformPermission(otherTenant, platformPerms)).toBe(false);
  });
  it("treats a missing tenant_id as the default tenant", () => {
    expect(hasPlatformPermission(legacyNoTenant, platformPerms)).toBe(true);
  });
  it("returns false on null claims", () => {
    expect(hasPlatformPermission(null, platformPerms)).toBe(false);
  });
  it("returns false on an empty permission list", () => {
    expect(hasPlatformPermission(defaultAdmin, [])).toBe(false);
  });
});
