import { describe, expect, it } from "vitest";
import { isActionAllowed, validBulkStates } from "./transitions";
import type { Record_ } from "./types";

function makeRow(state: string): Record_ {
  return { uid: "r1", state, host: "h1", date_epoch: 1 } as unknown as Record_;
}

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

describe("validBulkStates", () => {
  it("all-open rows → ack, close valid", () => {
    const rows = [makeRow(""), makeRow("open")];
    const valid = validBulkStates(rows);
    expect(valid.has("ack")).toBe(true);
    expect(valid.has("close")).toBe(true);
    expect(valid.has("open")).toBe(false);
  });

  it("all-acked rows → close, esc, open valid; ack absent", () => {
    const rows = [makeRow("ack"), makeRow("ack")];
    const valid = validBulkStates(rows);
    expect(valid.has("ack")).toBe(false);
    expect(valid.has("close")).toBe(true);
    expect(valid.has("esc")).toBe(true);
  });

  it("mixed open+acked rows → only close (intersection)", () => {
    const rows = [makeRow(""), makeRow("ack")];
    const valid = validBulkStates(rows);
    // open allows: ack, close; ack allows: close, esc, open → intersection = close
    expect(valid.has("close")).toBe(true);
    expect(valid.has("ack")).toBe(false);
    expect(valid.has("esc")).toBe(false);
  });

  it("all-closed rows → only open", () => {
    const rows = [makeRow("close"), makeRow("close")];
    const valid = validBulkStates(rows);
    expect(valid.has("open")).toBe(true);
    expect(valid.has("ack")).toBe(false);
    expect(valid.has("close")).toBe(false);
  });

  it("empty selection → empty set", () => {
    const valid = validBulkStates([]);
    expect(valid.size).toBe(0);
  });
});
