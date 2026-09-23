import { describe, expect, it } from "vitest";
import {
  CONFIDENCE_LEVELS,
  RISK_LEVELS,
  confidenceLabel,
  isConfidence,
  isRisk,
  riskLabel,
  riskTone,
} from "./enums";

describe("analysis enums", () => {
  it("lists confidence best-first and risk safest-first, matching the server's declaration order", () => {
    // Option lists render in this order; flipping either would silently
    // reverse every select in the editor.
    expect(CONFIDENCE_LEVELS).toEqual(["high", "medium", "low"]);
    expect(RISK_LEVELS).toEqual(["low", "medium", "high"]);
  });

  it("paints high risk as bad news", () => {
    expect(riskTone("low")).toBe("ok");
    expect(riskTone("medium")).toBe("warning");
    expect(riskTone("high")).toBe("critical");
  });

  it("labels every level, so colour is never the only carrier", () => {
    expect(CONFIDENCE_LEVELS.map(confidenceLabel)).toEqual(["High", "Medium", "Low"]);
    expect(RISK_LEVELS.map(riskLabel)).toEqual(["Low", "Medium", "High"]);
  });

  it("narrows only the accepted values", () => {
    for (const level of CONFIDENCE_LEVELS) expect(isConfidence(level)).toBe(true);
    for (const level of RISK_LEVELS) expect(isRisk(level)).toBe(true);

    // A value off the wire, a form's unset select, and the wrong enum entirely.
    expect(isConfidence("")).toBe(false);
    expect(isConfidence("HIGH")).toBe(false);
    expect(isConfidence(undefined)).toBe(false);
    expect(isConfidence(null)).toBe(false);
    expect(isConfidence(1)).toBe(false);

    expect(isRisk("")).toBe(false);
    expect(isRisk("critical")).toBe(false);
    expect(isRisk(undefined)).toBe(false);
  });
});
