import { describe, expect, it } from "vitest";
import { parseDuration } from "./duration";

describe("parseDuration", () => {
  it("parses single-unit durations", () => {
    expect(parseDuration("1h")).toBe(3600);
    expect(parseDuration("4h")).toBe(14400);
    expect(parseDuration("30m")).toBe(1800);
    expect(parseDuration("7d")).toBe(604800);
    expect(parseDuration("24h")).toBe(86400);
  });

  it("parses compound durations", () => {
    expect(parseDuration("2h30m")).toBe(9000);
    expect(parseDuration("1d12h")).toBe(129600);
    expect(parseDuration("1d12h30m")).toBe(131400);
  });

  it("tolerates surrounding whitespace and case", () => {
    expect(parseDuration("  2H30M ")).toBe(9000);
    expect(parseDuration("1D")).toBe(86400);
  });

  it("rejects invalid input with null", () => {
    expect(parseDuration("abc")).toBeNull();
    expect(parseDuration("")).toBeNull();
    expect(parseDuration("   ")).toBeNull();
    expect(parseDuration("5")).toBeNull(); // no unit
    expect(parseDuration("1x")).toBeNull(); // unknown unit
    expect(parseDuration("1.5h")).toBeNull(); // non-integer
    expect(parseDuration("1h foo")).toBeNull(); // trailing junk
    expect(parseDuration("h")).toBeNull(); // no number
    expect(parseDuration("0h")).toBeNull(); // zero total
  });
});
