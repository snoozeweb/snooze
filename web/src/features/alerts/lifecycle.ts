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
