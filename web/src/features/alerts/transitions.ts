import type { AlertState } from "./types";
import type { ActionType } from "./ActionDialog";

type GateableAction = ActionType | "shelve" | "unshelve";

const ALWAYS_ALLOWED: ReadonlySet<GateableAction> = new Set(["shelve", "unshelve", "comment"]);

// Keep in sync with: internal/pluginimpl/comment/transition.go::allowedTransitions.
// Mirror of the (current_state, action) validity table. A true value means the
// action is legal from that state. A state absent from the map, or an action
// absent from a present state's inner map, is treated as fail-open by
// isActionAllowed (returns true).
const ALLOWED: Readonly<Record<string, Readonly<Record<string, boolean>>>> = {
  // "" = fresh / open: may be acked or closed; nothing to re-open or esc yet.
  "": { ack: true, close: true, open: false, esc: false },
  // acked: may be closed, re-opened, or escalated; double-ack is rejected.
  ack: { ack: false, close: true, open: true, esc: true },
  // escalated: may be acked, closed, or re-opened; double-esc is rejected.
  esc: { ack: true, close: true, open: true, esc: false },
  // closed: only re-open makes sense.
  close: { ack: false, close: false, open: true, esc: false },
  // re-opened: may be acked or closed; re-open and esc are rejected.
  open: { ack: true, close: true, open: false, esc: false },
};

// eslint-disable-next-line @typescript-eslint/no-redundant-type-constituents
export function isActionAllowed(state: AlertState | string, action: GateableAction): boolean {
  if (ALWAYS_ALLOWED.has(action)) return true;
  const row = ALLOWED[state];
  if (!row) return true; // unknown state → fail-open
  const allowed = row[action];
  return allowed === undefined ? true : allowed; // unknown action → fail-open
}
