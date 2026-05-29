import { describe, it, expect } from "vitest";
import { throttleFromWire, throttleToWire } from "./throttle";

describe("throttle wire", () => {
  it("parses a scalar into default + no overrides", () => {
    expect(throttleFromWire(120)).toEqual({ defaultSeconds: 120, overrides: [] });
  });
  it("parses undefined into 0 default", () => {
    expect(throttleFromWire(undefined)).toEqual({ defaultSeconds: 0, overrides: [] });
  });
  it("splits a map into default + sorted overrides", () => {
    expect(throttleFromWire({ emergency: 120, critical: 86400, default: 3600 })).toEqual({
      defaultSeconds: 3600,
      overrides: [
        { value: "critical", seconds: 86400 },
        { value: "emergency", seconds: 120 },
      ],
    });
  });
  it("emits a scalar when there are no overrides", () => {
    expect(throttleToWire({ defaultSeconds: 120, overrides: [] })).toBe(120);
  });
  it("emits a map (with default key) when overrides exist", () => {
    expect(
      throttleToWire({ defaultSeconds: 3600, overrides: [{ value: "emergency", seconds: 120 }] }),
    ).toEqual({ emergency: 120, default: 3600 });
  });
  it("drops blank-value override rows when emitting", () => {
    expect(
      throttleToWire({ defaultSeconds: 0, overrides: [{ value: "  ", seconds: 5 }] }),
    ).toBe(0);
  });
});
