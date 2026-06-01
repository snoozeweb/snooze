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

export type StatsSnapshot = {
  by_state: Record<string, number>;
  total_hits: number;
  open: number;
  ack: number;
  closed: number;
};

export type StatsData = {
  series: StatsBucket[];
  totals: StatsTotals;
  snapshot: StatsSnapshot;
  weekday: Record<string, number>;
};

export type StatsResponse = {
  data: StatsData;
  meta: { from: string; to: string; bucket: number };
};
