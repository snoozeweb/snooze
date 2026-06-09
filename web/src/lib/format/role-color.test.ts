import { describe, expect, it } from "vitest";
import { isPlatformRole, roleBadgeVariant } from "./role-color";

describe("roleBadgeVariant", () => {
  it("gives the reserved platform_admin its own platform variant", () => {
    expect(roleBadgeVariant("platform_admin")).toBe("platform");
    expect(isPlatformRole("platform_admin")).toBe(true);
    expect(isPlatformRole("PLATFORM_ADMIN")).toBe(true);
    expect(isPlatformRole("admin")).toBe(false);
  });

  it("keeps admin distinct (critical) from platform_admin (platform)", () => {
    expect(roleBadgeVariant("admin")).toBe("critical");
    expect(roleBadgeVariant("platform_admin")).not.toBe(roleBadgeVariant("admin"));
  });

  it("maps common roles by keyword", () => {
    expect(roleBadgeVariant("oncall")).toBe("warning");
    expect(roleBadgeVariant("viewer")).toBe("muted");
    expect(roleBadgeVariant("anonymous")).toBe("neutral");
    expect(roleBadgeVariant("custom-thing")).toBe("neutral");
  });
});
