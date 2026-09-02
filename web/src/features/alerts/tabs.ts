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
import { SNOOZED_NOUN, STATE_NOUN } from "./lifecycle";

/**
 * TabId is the URL-safe identifier persisted in `?tab=…`. Stable: do not
 * rename without a migration shim.
 */
export type TabId = "alerts" | "snoozed" | "ack" | "esc" | "closed" | "shelved" | "all";

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
 * attention: not closed, not acknowledged, not currently snoozed, not shelved.
 *
 *   AND(NOT(state=ack), NOT(state=close), NOT(EXISTS snoozed),
 *       NOT(state=shelved), NOT(ttl < 0))
 *
 * The first three clauses match origin/master. The last two are what make
 * Shelve do what it says. A timed shelve parks the record in state=shelved
 * (comment type "shelve", see internal/pluginimpl/comment/transition.go); a
 * permanent shelve flips ttl negative (useShelveRecord/computeNextTTL in
 * api.ts). Without excluding both here a shelved row stayed put in this
 * default view — and kept being counted by useActiveAlertCount, which reuses
 * this exact preset. Mirrors the "Shelved" tab's two branches, so a shelved
 * alert leaves "Alerts" and appears under "Shelved" instead, whichever
 * mechanism shelved it.
 */
export const ACTIVE_ALERTS: Condition = {
  type: "AND",
  args: [
    { type: "NOT", arg: { type: "EQUALS", field: "state", value: "ack" } },
    { type: "NOT", arg: { type: "EQUALS", field: "state", value: "close" } },
    { type: "NOT", arg: { type: "EXISTS", field: "snoozed" } },
    { type: "NOT", arg: { type: "EQUALS", field: "state", value: "shelved" } },
    { type: "NOT", arg: { type: "LT", field: "ttl", value: 0 } },
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
 *
 * Mirrors the original Python YAML predicate:
 *
 *   OR(NOT(EXISTS ttl), ttl < 0)
 *
 * The "no ttl field at all" branch was dropped in the early Go rewrite
 * because the Go core was missing the per-record TTL stamp — every
 * fresh alert had no `ttl` and would have leaked into Shelved. Now that
 * internal/core/pipeline.go::stampDefaultTTL backfills the field at
 * ingest, the original two-branch predicate works again and still
 * matches pre-stamp legacy rows.
 */
const SHELVED: Condition = {
  type: "OR",
  args: [
    // New model: state field is the source of truth
    { type: "EQUALS", field: "state", value: "shelved" },
    // Legacy model: ttl<0 permanent shelve (ttl=-1) and pre-plan-34 rows
    { type: "NOT", arg: { type: "EXISTS", field: "ttl" } },
    { type: "LT", field: "ttl", value: 0 },
  ],
};

export const ALERT_TABS: TabDef[] = [
  { id: "alerts", label: "Alerts", condition: ACTIVE_ALERTS },
  { id: "snoozed", label: SNOOZED_NOUN, condition: { type: "EXISTS", field: "snoozed" } },
  { id: "ack", label: STATE_NOUN.ack, condition: { type: "EQUALS", field: "state", value: "ack" } },
  { id: "esc", label: STATE_NOUN.esc, condition: REESCALATED },
  {
    id: "closed",
    label: STATE_NOUN.close,
    condition: { type: "EQUALS", field: "state", value: "close" },
  },
  { id: "shelved", label: STATE_NOUN.shelved, condition: SHELVED },
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
