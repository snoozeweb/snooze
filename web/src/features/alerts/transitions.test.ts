import { describe, expect, it } from "vitest";
import {
  canTransition,
  describeBulkSkips,
  eligibleForBulkState,
  isActionAllowed,
  validBulkStates,
  type TransitionAction,
} from "./transitions";
import type { Record_ } from "./types";

// The source of truth: internal/pluginimpl/comment/transition.go's
// allowedTransitions map. The web UI must never offer a state-changing action
// the backend would reject, and must never hide one it would accept.
const BACKEND: Record<string, Record<TransitionAction, boolean>> = {
  "": { ack: true, close: true, open: false, esc: false },
  ack: { ack: false, close: true, open: true, esc: true },
  esc: { ack: true, close: true, open: true, esc: false },
  close: { ack: false, close: false, open: true, esc: false },
  open: { ack: true, close: true, open: false, esc: false },
};

describe("alert transition parity with the backend", () => {
  const actions: TransitionAction[] = ["ack", "close", "open", "esc"];
  for (const state of Object.keys(BACKEND)) {
    for (const action of actions) {
      const expected = BACKEND[state]![action];
      it(`state "${state}" ${expected ? "allows" : "rejects"} "${action}"`, () => {
        expect(canTransition(state, action)).toBe(expected);
      });
    }
  }

  it("fails open on an unknown state, matching the backend's ValidateTransition", () => {
    for (const action of ["ack", "close", "open", "esc"] as TransitionAction[]) {
      expect(canTransition("something-new", action)).toBe(true);
    }
  });
});

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

describe("eligibleForBulkState", () => {
  it("keeps only the rows the action is legal for", () => {
    const rows = [makeRow(""), makeRow("ack"), makeRow("close"), makeRow("esc")];
    expect(eligibleForBulkState(rows, "ack").length).toBe(2); // "" and esc
    expect(eligibleForBulkState(rows, "close").length).toBe(3); // "", ack, esc
    expect(eligibleForBulkState(rows, "open").length).toBe(3); // ack, close, esc
  });

  it("returns every row when they all qualify", () => {
    const rows = [makeRow(""), makeRow("open")];
    expect(eligibleForBulkState(rows, "ack")).toHaveLength(2);
  });

  it("returns nothing when no row qualifies", () => {
    const rows = [makeRow("close"), makeRow("close")];
    expect(eligibleForBulkState(rows, "ack")).toHaveLength(0);
  });

  it("empty selection → empty array", () => {
    expect(eligibleForBulkState([], "ack")).toHaveLength(0);
  });

  it("excludes rows in an unrecognised state (fail-closed for bulk)", () => {
    expect(eligibleForBulkState([makeRow("something-new")], "ack")).toHaveLength(0);
  });
});

describe("describeBulkSkips", () => {
  it("is empty when nothing is skipped", () => {
    expect(describeBulkSkips([makeRow(""), makeRow("open")], "ack")).toBe("");
  });

  it("names a single blocking state", () => {
    expect(describeBulkSkips([makeRow("ack"), makeRow("")], "ack")).toBe("already acknowledged");
  });

  it("joins several blocking states with 'or'", () => {
    const rows = [makeRow("ack"), makeRow("close"), makeRow("")];
    expect(describeBulkSkips(rows, "ack")).toBe("already acknowledged or already closed");
  });

  it("dedupes repeated states", () => {
    const rows = [makeRow("close"), makeRow("close"), makeRow("close")];
    expect(describeBulkSkips(rows, "close")).toBe("already closed");
  });

  it("falls back to the raw state for an unrecognised one", () => {
    expect(describeBulkSkips([makeRow("weird")], "ack")).toBe('in state "weird"');
  });
});
