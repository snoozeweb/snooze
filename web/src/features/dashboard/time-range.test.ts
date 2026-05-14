import { describe, expect, it } from "vitest";
import { presetToRange } from "./time-range";

describe("presetToRange", () => {
  it("returns a 24h window for '1d'", () => {
    const now = new Date("2026-05-14T12:00:00Z");
    const r = presetToRange("1d", now);
    expect(r.to).toBe("2026-05-14T12:00:00.000Z");
    expect(r.from).toBe("2026-05-13T12:00:00.000Z");
  });

  it("returns empty strings for 'custom'", () => {
    expect(presetToRange("custom").from).toBe("");
    expect(presetToRange("custom").to).toBe("");
  });

  it("returns ~30 days for '1m'", () => {
    const now = new Date("2026-05-14T00:00:00Z");
    const r = presetToRange("1m", now);
    expect(r.from).toBe("2026-04-14T00:00:00.000Z");
  });
});
