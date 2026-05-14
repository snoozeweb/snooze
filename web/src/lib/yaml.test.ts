import { describe, it, expect } from "vitest";
import { stableYaml } from "./yaml";

describe("stableYaml", () => {
  it("sorts top-level keys alphabetically for deterministic diffs", () => {
    const a = stableYaml({ z: 1, a: 2 });
    const b = stableYaml({ a: 2, z: 1 });
    expect(a).toBe(b);
    expect(a.indexOf("a:")).toBeLessThan(a.indexOf("z:"));
  });
  it("sorts nested keys too", () => {
    const out = stableYaml({ outer: { z: 1, a: 2 } });
    expect(out.indexOf("a:")).toBeLessThan(out.indexOf("z:"));
  });
  it("preserves array order", () => {
    const out = stableYaml({ items: [3, 1, 2] });
    expect(out).toContain("- 3");
    expect(out.indexOf("- 3")).toBeLessThan(out.indexOf("- 1"));
  });
  it("renders strings with double-quote style when ambiguous", () => {
    const out = stableYaml({ k: "true" });
    expect(out).toContain(`"true"`);
  });
});
