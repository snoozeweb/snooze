import { describe, expect, it } from "vitest";
import { escalationLabel } from "./format";

describe("escalationLabel", () => {
  // "Re-escalated", not "Escalated": the state badge beside it already reads
  // "Escalated" for state=esc, so the two must not read as the same fact.
  // Null rather than "" so a first-delivery alert renders no badge at all.
  it("returns null when the alert has never been re-escalated", () => {
    expect(escalationLabel(undefined)).toBeNull();
    expect(escalationLabel(0)).toBeNull();
    expect(escalationLabel(0, "timeout")).toBeNull();
  });

  it("drops the count for a single escalation", () => {
    expect(escalationLabel(1)).toBe("Re-escalated");
    expect(escalationLabel(1, "timeout")).toBe("Re-escalated (timeout)");
  });

  it("shows the count from the second escalation on", () => {
    expect(escalationLabel(2, "watchlist")).toBe("Re-escalated x2 (watchlist)");
    expect(escalationLabel(11, "timeout")).toBe("Re-escalated x11 (timeout)");
  });

  // For a manual escalation "who did this" is the question an operator asks
  // next, so attribution wins over the reason word.
  it("attributes a manual escalation to its operator", () => {
    expect(escalationLabel(2, "manual", "alice")).toBe("Re-escalated x2 by alice");
    expect(escalationLabel(2, "manual")).toBe("Re-escalated x2 (manual)");
  });
});
