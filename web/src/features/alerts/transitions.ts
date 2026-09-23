// Alert lifecycle transition rules — the frontend mirror of the backend's
// authoritative table in internal/pluginimpl/comment/transition.go
// (`allowedTransitions`). Keep the two in lockstep: the UI must only ever offer
// a state-changing action the server would accept. When they drift, operators
// get controls that silently 403 (the doomed "Re-escalate" on fresh/re-opened
// rows) — or, worse, escalated alerts with no ack/close/re-open control at all.
import type { ActionType } from "./ActionDialog";
import type { AlertState, Record_ } from "./types";

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

// ── Action gating (kebab menu / quick actions / context menu / bulk toolbar) ──
// Superset of TransitionAction: also covers the non-state-changing "comment"
// action and the timed shelve/unshelve pair, which are always offered
// regardless of state (the backend never rejects them on state grounds).

export type GateableAction = ActionType | "shelve" | "unshelve";

const ALWAYS_ALLOWED: ReadonlySet<GateableAction> = new Set(["shelve", "unshelve", "comment"]);

/**
 * Shown in the bulk-action success toast to warn operators that bulk state
 * changes do not write per-alert activity entries (unlike the single-alert
 * /comment path). Directs them to the Audit log.
 */
export const BULK_STATE_CAVEAT =
  "No per-alert activity entry was written — see Audit log for details.";

// Valid target states for each source state.
// Keep in sync with ALLOWED above and the backend transition.go.
const VALID_FROM: Readonly<Record<string, ActionType[]>> = {
  "": ["ack", "close"],
  open: ["ack", "close"],
  ack: ["close", "esc", "open"],
  esc: ["ack", "close", "open"],
  close: ["open"],
  shelved: ["open"],
};

/** State → the phrase that explains why a row is being skipped, e.g. an
 *  already-acked row skipped by a bulk Acknowledge reads "already
 *  acknowledged". Used to name the reason in the bulk bar's tooltip, the
 *  confirm dialog, and the result toast. */
const SKIP_LABEL: Readonly<Record<string, string>> = {
  "": "still open",
  open: "still open",
  ack: "already acknowledged",
  esc: "already re-escalated",
  close: "already closed",
  shelved: "shelved",
};

/**
 * Rows in `rows` for which `action` is a legal transition — the subset a bulk
 * Acknowledge/Close should actually apply to.
 *
 * The bulk bar used to hide a button whenever ONE selected row disagreed
 * (validBulkStates is an intersection), so select-all on the "All" tab made
 * Acknowledge and Close vanish with no explanation. Acting on the eligible
 * subset and naming the skipped rest is the honest version of that.
 */
export function eligibleForBulkState(rows: Record_[], action: ActionType): Record_[] {
  return rows.filter((r) => (VALID_FROM[(r.state ?? "") as AlertState] ?? []).includes(action));
}

/**
 * Names why the ineligible rows in `rows` can't take `action`, as a clause
 * that slots into "3 skipped (…)" or "None of the selected alerts can be
 * acknowledged — …". Returns "" when every row is eligible.
 */
export function describeBulkSkips(rows: Record_[], action: ActionType): string {
  const labels: string[] = [];
  for (const r of rows) {
    const state = (r.state ?? "") as string;
    if ((VALID_FROM[state as AlertState] ?? []).includes(action)) continue;
    const label = SKIP_LABEL[state] ?? `in state "${state}"`;
    if (!labels.includes(label)) labels.push(label);
  }
  if (labels.length === 0) return "";
  if (labels.length === 1) return labels[0]!;
  return `${labels.slice(0, -1).join(", ")} or ${labels[labels.length - 1]!}`;
}

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

/**
 * isActionAllowed reports whether `action` is legal from a record's current
 * `state`. Shares the ALLOWED transition table above for the state-changing
 * actions; comment/shelve/unshelve are always allowed regardless of state.
 * Unknown states and unknown actions both fail open, matching the backend.
 */
// eslint-disable-next-line @typescript-eslint/no-redundant-type-constituents
export function isActionAllowed(state: AlertState | string, action: GateableAction): boolean {
  if (ALWAYS_ALLOWED.has(action)) return true;
  const row = ALLOWED[state];
  if (!row) return true; // unknown state → fail-open
  const allowed = (row as Record<string, boolean>)[action];
  return allowed === undefined ? true : allowed; // unknown action → fail-open
}

// ── Ownership (assign / release) ──────────────────────────────────────────────
// Not state transitions — the backend keeps them out of allowedTransitions —
// but gated all the same, by the comment plugin's GuardWrite: assigning a
// closed alert and releasing an unowned one are both refused with a 403.

/** Whether "Assign to…" is offered: anything that is not closed. */
export function canAssign(row: Record_): boolean {
  return (row.state ?? "") !== "close";
}

/** Whether "Release" is offered: only when somebody owns the alert. A row with
 *  just a previous owner is already unowned — there is nothing to give back. */
export function canRelease(row: Record_): boolean {
  return (row.owner ?? "") !== "";
}
