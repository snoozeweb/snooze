import { describe, expect, it } from "vitest";
import { allowedTransitionActions, canTransition, type TransitionAction } from "./transitions";

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

  it("makes escalated alerts actionable (ack/close/re-open), never re-escalate", () => {
    // The headline bug: state "esc" previously matched none of the row-action
    // branches, leaving escalated alerts unresolvable from the row UI.
    expect(allowedTransitionActions("esc")).toEqual(["ack", "close", "open"]);
  });

  it("never offers Re-escalate on fresh or re-opened alerts (the always-403 button)", () => {
    expect(allowedTransitionActions("")).toEqual(["ack", "close"]);
    expect(allowedTransitionActions("open")).toEqual(["ack", "close"]);
  });

  it("offers Re-open on acknowledged alerts (previously missing)", () => {
    expect(allowedTransitionActions("ack")).toEqual(["close", "esc", "open"]);
  });

  it("offers only Re-open on closed alerts", () => {
    expect(allowedTransitionActions("close")).toEqual(["open"]);
  });

  it("fails open on an unknown state, matching the backend's ValidateTransition", () => {
    for (const action of ["ack", "close", "open", "esc"] as TransitionAction[]) {
      expect(canTransition("something-new", action)).toBe(true);
    }
  });
});
