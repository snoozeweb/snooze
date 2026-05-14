export type StatsRange = "1d" | "1w" | "1m" | "1y" | "custom";

export type StatsBucket = {
  t: string;
  counts: Record<string, number>;
};

export type StatsTotals = {
  by_severity: Record<string, number>;
  by_environment: Record<string, number>;
  by_action_success: Record<string, number>;
  by_action_failure: Record<string, number>;
};

export type StatsData = {
  series: StatsBucket[];
  totals: StatsTotals;
};

export type StatsResponse = {
  data: StatsData;
  meta: { from: string; to: string; bucket: number };
};
