import { describe, expect, it } from "vitest";
import { clusterVerdict } from "./verdict";

describe("clusterVerdict", () => {
  it("reports all-clear when every member is ok and every plugin loaded", () => {
    const v = clusterVerdict({
      cluster: { members: [{ name: "a", status: "ok" }] },
      plugins: [{ name: "rule", loaded: true }],
    });
    expect(v).toEqual({ tone: "ok", issues: 0, label: "All systems operational" });
  });

  it("flags a degraded member or unloaded plugin as a warning and counts them", () => {
    const v = clusterVerdict({
      cluster: { members: [{ name: "a", status: "degraded" }] },
      plugins: [{ name: "rule", loaded: false }],
    });
    expect(v.tone).toBe("warning");
    expect(v.issues).toBe(2);
    expect(v.label).toMatch(/2 issues detected/i);
  });

  it("escalates to critical when a member is down", () => {
    const v = clusterVerdict({ cluster: { members: [{ name: "a", status: "down" }] } });
    expect(v.tone).toBe("critical");
    expect(v.label).toMatch(/1 issue detected/i);
  });

  it("treats missing data as all-clear rather than crashing", () => {
    expect(clusterVerdict({}).tone).toBe("ok");
  });

  it("treats an unknown member status as critical, matching the row badge", () => {
    // Defends against a future backend status the enum doesn't list: the banner
    // must not claim all-clear while the table paints the row red.
    const v = clusterVerdict({
      cluster: { members: [{ name: "a", status: "unreachable" as never }] },
    });
    expect(v.tone).toBe("critical");
    expect(v.issues).toBe(1);
  });
});
