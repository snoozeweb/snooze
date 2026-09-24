import { describe, expect, it } from "vitest";
import { cadenceLabel, repeatLabel } from "./cadence";

describe("cadenceLabel", () => {
  it("rounds to the unit an operator would say", () => {
    expect(cadenceLabel(45)).toBe("every ~45 s");
    expect(cadenceLabel(960)).toBe("every ~16 min");
    expect(cadenceLabel(89)).toBe("every ~1 min");
    expect(cadenceLabel(2 * 3600 + 600)).toBe("every ~2 h");
    expect(cadenceLabel(3 * 86400)).toBe("every ~3 d");
  });
  it("says nothing without a cadence", () => {
    expect(cadenceLabel(0)).toBe("");
    expect(cadenceLabel(undefined)).toBe("");
  });
});

describe("repeatLabel", () => {
  it("groups thousands", () => {
    expect(repeatLabel(1420)).toBe("×1,420");
    expect(repeatLabel(3)).toBe("×3");
  });
});
