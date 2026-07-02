import { describe, expect, it } from "vitest";
import type { JwtClaims } from "@/lib/auth/jwt";
import { firstLandingPath, visibleNavItems } from "./nav-list";

function claims(permissions: string[]): JwtClaims {
  return { permissions } as unknown as JwtClaims;
}

describe("firstLandingPath", () => {
  it("lands a record user on Alerts (the first permitted nav item)", () => {
    expect(firstLandingPath(claims(["ro_record"]))).toBe("/web/alerts");
  });

  it("lands a stats-only user on their first permitted page, not the record-gated Alerts", () => {
    const dest = firstLandingPath(claims(["ro_stats"]));
    expect(dest).not.toBe("/web/alerts");
    // Dashboard is the first stats-gated nav item.
    expect(dest).toBe("/web/dashboard");
    expect(visibleNavItems(claims(["ro_stats"])).some((i) => i.to === dest)).toBe(true);
  });

  it("falls back to a visible (permission-free) page for a permission-less user", () => {
    const dest = firstLandingPath(claims([]));
    // Whatever it is, the user can actually see it.
    expect(visibleNavItems(claims([])).some((i) => i.to === dest)).toBe(true);
  });
});
