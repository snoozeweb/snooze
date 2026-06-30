import { describe, expect, it } from "vitest";
import { isActionAllowed } from "./transitions";

describe("isActionAllowed — table-driven (20 cells)", () => {
  it.each([
    // "" (fresh/open)
    ["", "ack", true],
    ["", "close", true],
    ["", "open", false],
    ["", "esc", false],
    // ack
    ["ack", "ack", false],
    ["ack", "close", true],
    ["ack", "open", true],
    ["ack", "esc", true],
    // esc
    ["esc", "ack", true],
    ["esc", "close", true],
    ["esc", "open", true],
    ["esc", "esc", false],
    // close
    ["close", "ack", false],
    ["close", "close", false],
    ["close", "open", true],
    ["close", "esc", false],
    // open
    ["open", "ack", true],
    ["open", "close", true],
    ["open", "open", false],
    ["open", "esc", false],
  ] as const)("isActionAllowed(%s, %s) → %s", (state, action, expected) => {
    expect(isActionAllowed(state, action)).toBe(expected);
  });
});

describe("isActionAllowed — always-allowed actions", () => {
  const states = ["", "ack", "esc", "close", "open", "shelved"] as const;

  it.each(states)("shelve is always allowed from state '%s'", (state) => {
    expect(isActionAllowed(state, "shelve")).toBe(true);
  });

  it.each(states)("unshelve is always allowed from state '%s'", (state) => {
    expect(isActionAllowed(state, "unshelve")).toBe(true);
  });
});

describe("isActionAllowed — comment always true", () => {
  it.each(["", "ack", "esc", "close", "open"] as const)(
    "comment is always allowed from state '%s'",
    (state) => {
      expect(isActionAllowed(state, "comment")).toBe(true);
    },
  );
});

describe("isActionAllowed — unknown state fail-open", () => {
  it("unknown state returns true for a known gated action", () => {
    expect(isActionAllowed("unknown_state", "ack")).toBe(true);
  });
});

describe("isActionAllowed — unknown action fail-open", () => {
  it("unknown action returns true from a known state", () => {
    // "delete" is not a GateableAction; cast needed to pass the type check
    expect(isActionAllowed("ack", "delete" as Parameters<typeof isActionAllowed>[1])).toBe(true);
  });
});
