import { describe, expect, it } from "vitest";
import { summarizeFrequency } from "./FrequencyEditor";

describe("summarizeFrequency", () => {
  it("returns 'Once' for undefined frequency", () => {
    expect(summarizeFrequency(undefined)).toBe("Once");
  });

  it("returns 'Once' for empty object", () => {
    expect(summarizeFrequency({})).toBe("Once");
  });

  it("returns 'Once' when all fields are zero/falsy", () => {
    expect(summarizeFrequency({ total: 0, delay: 0, every: 0 })).toBe("Once");
  });

  it("summarizes a total-only frequency", () => {
    expect(summarizeFrequency({ total: 3 })).toBe("×3");
  });

  it("summarizes total + every + delay in canonical order", () => {
    expect(summarizeFrequency({ total: 2, delay: 30, every: 600 })).toBe(
      "×2 every 600s +30s",
    );
  });
});
