// Canonical lifecycle-state vocabulary, shared by every surface that names
// an alert's state: state chip, tab label, dashboard tile, chart legend,
// timeline. One noun per state so "Ack" / "Acknowledged" / "acknowledged"
// drift can't creep back in across files.
//
// Zero imports on purpose — both `format.ts` and `tabs.ts` need this map,
// and format.ts <-> tabs.ts must not import each other (import-cycle risk).
import type { AlertState } from "./types";

export const STATE_NOUN: Record<AlertState, string> = {
  "": "Open",
  open: "Open",
  ack: "Acknowledged",
  esc: "Re-escalated",
  close: "Closed",
  shelved: "Shelved",
};

// Snoozed is a flag (a future-dated re-open), not a lifecycle state — it
// isn't a key in AlertState/STATE_NOUN. Exported here anyway so every
// surface that names it (tab, tile, chart legend) shares one literal.
export const SNOOZED_NOUN = "Snoozed";

// Ownership vocabulary — who is working on an alert, as opposed to what state
// it is in. The Owner column, the owner filter, the row inspector, the action
// menus and the timeline all name these the same way.
export const OWNER_NOUN = "Owner";
export const PREVIOUS_OWNER_NOUN = "Previous owner";
// The owner filter's chip for alerts nobody has — including ones that only
// carry a previous owner, which is the server's definition too.
export const UNOWNED_NOUN = "Unowned";
// Menu verbs. The ellipsis is load-bearing: Assign asks who before it acts.
export const ASSIGN_VERB = "Assign to…";
export const RELEASE_VERB = "Release";
