// Alert lifecycle transition rules — the frontend mirror of the backend's
// authoritative table in internal/pluginimpl/comment/transition.go
// (`allowedTransitions`). Keep the two in lockstep: the UI must only ever offer
// a state-changing action the server would accept. When they drift, operators
// get controls that silently 403 (the doomed "Re-escalate" on fresh/re-opened
// rows) — or, worse, escalated alerts with no ack/close/re-open control at all.

export type TransitionAction = "ack" | "close" | "esc" | "open";

// [currentState][action] → is the action legal from that state?
// Byte-for-byte the backend's allowedTransitions map.
const ALLOWED: Record<string, Record<TransitionAction, boolean>> = {
  // "" = fresh / open: may be acked or closed; nothing to re-open or esc yet.
  "": { ack: true, close: true, esc: false, open: false },
  // acked: may be closed, re-escalated, or re-opened; double-ack is rejected.
  ack: { ack: false, close: true, esc: true, open: true },
  // escalated: may be acked, closed, or re-opened; double-esc is rejected.
  esc: { ack: true, close: true, esc: false, open: true },
  // closed: only re-open makes sense.
  close: { ack: false, close: false, esc: false, open: true },
  // re-opened: may be acked or closed; re-open and esc are rejected.
  open: { ack: true, close: true, esc: false, open: false },
};

// Canonical order the actions appear in menus/toolbars.
const ORDER: TransitionAction[] = ["ack", "close", "esc", "open"];

/**
 * canTransition reports whether `action` is legal from a record's current
 * `state`, mirroring the backend's ValidateTransition. Unknown states fail
 * open (return true), exactly as the backend does, so the UI never hides an
 * action the server would actually accept.
 */
export function canTransition(state: string, action: TransitionAction): boolean {
  const row = ALLOWED[state];
  if (!row) return true; // fail-open on an unknown state, matching the backend
  return row[action];
}

/**
 * allowedTransitionActions lists the state-changing actions to offer for
 * `state`, in canonical menu order.
 */
export function allowedTransitionActions(state: string): TransitionAction[] {
  return ORDER.filter((action) => canTransition(state, action));
}
