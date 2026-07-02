import type { StatsRange } from "./types";

const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const pad = (n: number) => String(n).padStart(2, "0");

/**
 * formatBucketLabel renders a bucket's ISO timestamp as a readable axis-tick /
 * tooltip label at the grain that matches the selected range, replacing the
 * raw RFC3339 string ("2026-06-25T14:00:00Z") that was previously shown. Uses
 * local wall-clock time, consistent with the rest of the app's time display.
 */
export function formatBucketLabel(iso: string, range: StatsRange): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso; // never worse than the raw value
  const hm = `${pad(d.getHours())}:${pad(d.getMinutes())}`;
  const md = `${MONTHS[d.getMonth()]} ${d.getDate()}`;
  switch (range) {
    case "1d":
      return hm; // one day → time of day is unambiguous
    case "1w":
      return `${WEEKDAYS[d.getDay()]} ${hm}`; // weekday + time, unambiguous within a week
    case "1y":
      return md; // daily buckets → date grain
    case "1m":
      // 6h buckets: date alone would collapse four buckets to one label and
      // make the tooltip ambiguous, so keep the time of day.
      return `${md}, ${hm}`;
    default:
      return `${md}, ${hm}`; // custom range → full date + time
  }
}

/**
 * capDistributions keeps only the top-N keys (ranked by summed value across all
 * given series) and returns each series restricted to those keys. High-
 * cardinality bar panels (throttled-by-rule, snoozed-by-filter, actions-by-
 * name) would otherwise render dozens of unreadable slivers with auto-skipped
 * labels; this caps them client-side, mirroring the server-side top-10
 * treatment of the by_host panel.
 */
export function capDistributions(
  series: Array<Record<string, number>>,
  n: number,
): { keys: string[]; capped: Array<Record<string, number>>; total: number } {
  const totals = new Map<string, number>();
  for (const s of series) {
    for (const [k, v] of Object.entries(s)) totals.set(k, (totals.get(k) ?? 0) + v);
  }
  const keys = [...totals.entries()]
    .sort((a, b) => b[1] - a[1])
    .slice(0, n)
    .map(([k]) => k);
  const keySet = new Set(keys);
  const capped = series.map((s) => {
    const out: Record<string, number> = {};
    for (const [k, v] of Object.entries(s)) if (keySet.has(k)) out[k] = v;
    return out;
  });
  // `total` is the distinct-key count BEFORE capping, so callers can honestly
  // say "Top N of total" when the panel is truncated.
  return { keys, capped, total: totals.size };
}
