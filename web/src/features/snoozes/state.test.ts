import { describe, expect, it } from "vitest";
import { snoozeState } from "./state";

const FIXED_NOW = 1747300000; // arbitrary epoch
const PAST = new Date((FIXED_NOW - 3600) * 1000).toISOString();
const FUTURE = new Date((FIXED_NOW + 3600) * 1000).toISOString();

describe("snoozeState", () => {
  it("returns 'active' when no datetime constraints are present", () => {
    expect(snoozeState({ name: "x" }, FIXED_NOW)).toBe("active");
    expect(snoozeState({ name: "x", time_constraints: {} }, FIXED_NOW)).toBe("active");
    // Weekdays-only or time-only constraint is still 'active' — those are
    // about recurrence, not lifespan.
    expect(
      snoozeState(
        {
          name: "x",
          time_constraints: { weekdays: [{ weekdays: [1, 2, 3, 4, 5] }] },
        },
        FIXED_NOW,
      ),
    ).toBe("active");
  });
  it("returns 'expired' when every datetime range's until is in the past", () => {
    expect(
      snoozeState(
        {
          name: "x",
          time_constraints: { datetime: [{ until: PAST }] },
        },
        FIXED_NOW,
      ),
    ).toBe("expired");
  });
  it("returns 'upcoming' when every datetime range's from is in the future", () => {
    expect(
      snoozeState(
        {
          name: "x",
          time_constraints: { datetime: [{ from: FUTURE }] },
        },
        FIXED_NOW,
      ),
    ).toBe("upcoming");
  });
  it("returns 'active' when a range straddles now (from past, until future)", () => {
    expect(
      snoozeState(
        {
          name: "x",
          time_constraints: { datetime: [{ from: PAST, until: FUTURE }] },
        },
        FIXED_NOW,
      ),
    ).toBe("active");
  });
  it("returns 'active' when one of multiple ranges is current", () => {
    // Two ranges: one ended yesterday, one current. UI shouldn't call this
    // expired — the snooze is still doing something.
    expect(
      snoozeState(
        {
          name: "x",
          time_constraints: {
            datetime: [{ until: PAST }, { from: PAST, until: FUTURE }],
          },
        },
        FIXED_NOW,
      ),
    ).toBe("active");
  });
});
