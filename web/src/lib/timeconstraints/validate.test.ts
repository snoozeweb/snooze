import { describe, expect, it } from "vitest";
import { timeConstraintsError } from "./validate";

describe("timeConstraintsError", () => {
  it("returns null for an empty group (always-on is legitimate)", () => {
    expect(timeConstraintsError({})).toBeNull();
  });

  it("flags a both-empty absolute date range (the never-matching trap)", () => {
    expect(timeConstraintsError({ datetime: [{ from: "", until: "" }] })).toMatch(/date range/i);
  });

  it("allows a half-open (from-only) absolute range — a valid ray, not the trap", () => {
    // Backend Match treats from-set/until-nil as "t >= from"; react-day-picker's
    // first click legitimately produces this shape.
    expect(timeConstraintsError({ datetime: [{ from: "2026-07-02T09:00" }] })).toBeNull();
    expect(timeConstraintsError({ datetime: [{ until: "2026-07-02T17:00" }] })).toBeNull();
  });

  it("accepts a complete absolute range", () => {
    expect(
      timeConstraintsError({ datetime: [{ from: "2026-07-02T09:00", until: "2026-07-02T17:00" }] }),
    ).toBeNull();
  });

  it("flags a both-empty time-of-day window", () => {
    expect(timeConstraintsError({ time: [{ from: "", until: "" }] })).toMatch(/time-of-day/i);
  });

  it("allows a half-open time-of-day window (from-only)", () => {
    expect(timeConstraintsError({ time: [{ from: "09:00" }] })).toBeNull();
  });

  it("accepts a weekdays-only group", () => {
    expect(timeConstraintsError({ weekdays: [{ weekdays: [1, 2, 3] }] })).toBeNull();
  });
});
