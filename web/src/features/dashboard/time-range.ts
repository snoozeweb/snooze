import type { StatsRange } from "./types";

export type TimeRange = {
  range: StatsRange;
  from: string;
  to: string;
};

export function presetToRange(
  range: StatsRange,
  now: Date = new Date(),
): { from: string; to: string } {
  if (range === "custom") return { from: "", to: "" };
  const ms: Record<Exclude<StatsRange, "custom">, number> = {
    "1d": 86_400_000,
    "1w": 7 * 86_400_000,
    "1m": 30 * 86_400_000,
    "1y": 365 * 86_400_000,
  };
  const to = now.toISOString();
  const from = new Date(now.getTime() - ms[range]).toISOString();
  return { from, to };
}
