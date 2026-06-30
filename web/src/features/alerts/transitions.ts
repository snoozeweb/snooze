import type { AlertState } from "./types";
import type { ActionType } from "./ActionDialog";
import type { Record_ } from "./types";

type GateableAction = ActionType | "shelve" | "unshelve";

const ALWAYS_ALLOWED: ReadonlySet<GateableAction> = new Set(["shelve", "unshelve", "comment"]);

/**
 * Shown in the bulk-action success toast to warn operators that bulk state
 * changes do not write per-alert activity entries (unlike the single-alert
 * /comment path). Directs them to the Audit log.
 */
export const BULK_STATE_CAVEAT =
  "No per-alert activity entry was written — see Audit log for details.";

// Valid target states for each source state.
// Keep in sync with ALLOWED below and the backend transition.go.
const VALID_FROM: Readonly<Record<string, ActionType[]>> = {
  "": ["ack", "close"],
  open: ["ack", "close"],
  ack: ["close", "esc", "open"],
  esc: ["ack", "close", "open"],
  close: ["open"],
  shelved: ["open"],
};

/**
 * Returns the set of ActionTypes that are valid for ALL rows in the selection.
 * "comment" and "tag" are always valid and are not returned here (callers add
 * them unconditionally). An empty selection returns an empty set.
 */
export function validBulkStates(rows: Record_[]): Set<ActionType> {
  if (rows.length === 0) return new Set<ActionType>();
  const sets = rows.map(
    (r) => new Set<ActionType>(VALID_FROM[(r.state ?? "") as AlertState] ?? []),
  );
  const first = new Set<ActionType>(sets[0]);
  for (const action of [...first]) {
    if (!sets.every((s) => s.has(action))) first.delete(action);
  }
  return first;
}

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
