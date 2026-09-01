export type StatsRange = "1d" | "1w" | "1m" | "1y" | "custom";

export type StatsBucket = {
  t: string;
  counts: Record<string, number>;
};

export type StatsTotals = {
  by_severity: Record<string, number>;
  by_environment: Record<string, number>;
  by_host: Record<string, number>;
  by_action_success: Record<string, number>;
  by_action_failure: Record<string, number>;
  by_throttled: Record<string, number>;
  by_snoozed: Record<string, number>;
  by_notification: Record<string, number>;
};

/**
 * Live state of the alert store *right now* — not scoped to the picked
 * window, so these counts match what the alerts table lists. `total_hits` is
 * the odd one out: it is the counter-backed count of events ingested in the
 * window (duplicates included) and must always be labelled with that window.
 */
export type StatsSnapshot = {
  by_state: Record<string, number>;
  total_hits: number;
  open: number;
  ack: number;
  closed: number;
};

/**
 * Provenance for the counter-backed half of the response. Lets the UI word
 * three very different empty dashboards honestly instead of printing the same
 * "No data." for all of them.
 */
export type StatsCounters = {
  /** general.metrics_enabled — false means counters are never written. */
  enabled: boolean;
  /** At least one counter document exists, in this window or any other. */
  present: boolean;
};

export type StatsData = {
  series: StatsBucket[];
  totals: StatsTotals;
  snapshot: StatsSnapshot;
  weekday: Record<string, number>;
};

export type StatsResponse = {
  data: StatsData;
  // `counters` is optional on the wire so a UI built after a server upgrade
  // still renders against an older backend (it degrades to "quiet window").
  meta: { from: string; to: string; bucket: number; counters?: StatsCounters };
};
