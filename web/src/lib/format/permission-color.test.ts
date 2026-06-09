import { describe, expect, it } from "vitest";
import { permissionBadgeVariant } from "./permission-color";

describe("permissionBadgeVariant", () => {
  it("distinguishes rw_ (warning) from ro_ (info)", () => {
    expect(permissionBadgeVariant("rw_rule")).toBe("warning");
    expect(permissionBadgeVariant("ro_rule")).toBe("info");
    expect(permissionBadgeVariant("rw_rule")).not.toBe(permissionBadgeVariant("ro_rule"));
  });

  it("flags rw_all and admin_* as critical", () => {
    expect(permissionBadgeVariant("rw_all")).toBe("critical");
    expect(permissionBadgeVariant("admin_users")).toBe("critical");
  });

  it("treats platform-tier perms (ro_tenant / rw_tenant) as warning", () => {
    expect(permissionBadgeVariant("ro_tenant")).toBe("warning");
    expect(permissionBadgeVariant("rw_tenant")).toBe("warning");
  });

  it("mutes deny_* and anonymous", () => {
    expect(permissionBadgeVariant("deny_rule")).toBe("muted");
    expect(permissionBadgeVariant("anonymous")).toBe("muted");
  });

  it("falls back to neutral for unknown shapes", () => {
    expect(permissionBadgeVariant("weird")).toBe("neutral");
  });
});
