import { describe, expect, it } from "vitest";
import { hasAllPermissions, hasAnyPermission, hasPermission } from "./permissions";
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
