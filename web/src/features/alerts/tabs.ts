// Alert lifecycle tabs.
//
// Each tab is a preset Condition that filters the record collection by
// lifecycle state. The 7-tab set mirrors the Python 1.x web UI
// (src/snooze/defaults/web/alert.yaml) — that layout has years of
// operator feedback baked in and we want the new Go UI to feel the same.
//
// Tab presets AND-combine with the SearchBar's DSL condition in
// AlertsPage. An empty `condition` (Alerts: null) means the tab adds no
// constraint beyond the DSL itself.

import type { Condition } from "@/lib/condition/types";

/**
 * TabId is the URL-safe identifier persisted in `?tab=…`. Stable: do not
 * rename without a migration shim.
 */
export type TabId =
  | "alerts"
  | "snoozed"
  | "ack"
  | "esc"
  | "closed"
  | "shelved"
  | "all";

export type TabDef = {
  id: TabId;
  label: string;
  /**
   * Preset condition for the tab, or null when the tab applies no
   * constraint (e.g. "All"). Combined with the SearchBar's DSL condition
   * via AND in AlertsPage.
   */
  condition: Condition | null;
};

/**
 * "Alerts" — the default landing tab, showing active alerts that need
 * attention: not closed, not acknowledged, not currently snoozed. The
 * three-clause AND matches origin/master:
 *
 *   AND(NOT(state=ack), NOT(state=close), NOT(EXISTS snoozed))
 */
const ACTIVE_ALERTS: Condition = {
  type: "AND",
  args: [
    { type: "NOT", arg: { type: "EQUALS", field: "state", value: "ack" } },
    { type: "NOT", arg: { type: "EQUALS", field: "state", value: "close" } },
    { type: "NOT", arg: { type: "EXISTS", field: "snoozed" } },
  ],
};

/**
 * "Re-escalated" — records the operator pulled out of acknowledged/closed
 * back into the open queue (state=esc) plus everything currently flagged
 * open (state=open). The Python rule was: state IN ("esc","open").
 */
const REESCALATED: Condition = {
  type: "OR",
  args: [
    { type: "EQUALS", field: "state", value: "esc" },
    { type: "EQUALS", field: "state", value: "open" },
  ],
};

/**
 * "Shelved" — items the operator has soft-hidden from default views.
 * The Python YAML wrote `NOT EXISTS ttl OR ttl<0`, but the `NOT EXISTS ttl`
 * branch would match almost every fresh alert (most records never set a
 * ttl), and the Vue Record.vue's `can_be_shelved` only flips items where
 * `ttl >= 0` to negative. We keep the meaningful half (`ttl<0`) and drop
 * the noisy branch.
 */
const SHELVED: Condition = { type: "LT", field: "ttl", value: 0 };

export const ALERT_TABS: TabDef[] = [
  { id: "alerts", label: "Alerts", condition: ACTIVE_ALERTS },
  { id: "snoozed", label: "Snoozed", condition: { type: "EXISTS", field: "snoozed" } },
  { id: "ack", label: "Acknowledged", condition: { type: "EQUALS", field: "state", value: "ack" } },
  { id: "esc", label: "Re-escalated", condition: REESCALATED },
  { id: "closed", label: "Closed", condition: { type: "EQUALS", field: "state", value: "close" } },
  { id: "shelved", label: "Shelved", condition: SHELVED },
  { id: "all", label: "All", condition: null },
];

/**
 * Look up a tab by id. Falls back to the default "alerts" tab when the
 * id is unknown — keeps URL deep-links robust against typos or future
 * renames.
 */
export function tabById(id: string | undefined): TabDef {
  if (!id) return ALERT_TABS[0]!;
  const found = ALERT_TABS.find((t) => t.id === id);
  return found ?? ALERT_TABS[0]!;
}
