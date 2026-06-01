import { useMemo, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { Card } from "@/shared/ui/Card";
import { Spinner } from "@/shared/ui/Spinner";
import { BarChart } from "@/shared/chart/BarChart";
import { DonutChart } from "@/shared/chart/DonutChart";
import { LineChart, type LineSeries } from "@/shared/chart/LineChart";
import { severityColors } from "@/lib/format/severity-color";
import { useStats } from "./api";
import { TimeRangePicker } from "./TimeRangePicker";
import { presetToRange, type TimeRange } from "./time-range";
import { StatTiles } from "./StatTiles";
import { ActivityFeed } from "./ActivityFeed";
import { alertsSearchForBucket } from "./bucket-utils";
import styles from "./DashboardPage.module.css";

// Keys must match the exact series-key strings the backend /stats emits in series[].counts —
// a backend rename will silently drop the series here, making the mismatch visible.
const LINE_COLORS: Record<string, string> = {
  Alerts: "#4f8cff",
  Throttled: "#8957e5",
  Snoozed: "#d4a017",
  "Notification sent": "#3fb950",
  "Action error": "#f04949",
};

const WEEKDAY_LABELS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"] as const;
const WEEKDAY_KEYS = ["1", "2", "3", "4", "5", "6", "0"] as const;

export function DashboardPage() {
  const navigate = useNavigate();
  const [range, setRange] = useState<TimeRange>(() => {
    const r = presetToRange("1d");
    return { range: "1d", from: r.from, to: r.to };
  });
  const bucket = bucketFromRange(range.range);
  const stats = useStats({ from: range.from, to: range.to, bucket });

  const data = stats.data?.data;

  const lineSeries: LineSeries[] = useMemo(() => {
    const series = data?.series;
    if (!Array.isArray(series) || series.length === 0) return [];
    // Collect all keys that appear in any bucket, in fixed order
    const keysPresent = new Set<string>();
    for (const b of series) {
      for (const k of Object.keys(b.counts ?? {})) keysPresent.add(k);
    }
    // Emit only keys that have a known color, in the canonical fixed order
    return Object.keys(LINE_COLORS)
      .filter((k) => keysPresent.has(k))
      .map((key) => ({
        label: key,
        color: LINE_COLORS[key]!,
        data: series.map((b) => ({ x: b.t, y: b.counts[key] ?? 0 })),
      }));
  }, [data]);

  // Any series/point click navigates to the alerts in that time bucket (series-agnostic).
  const handlePointClick = (_seriesLabel: string, x: string) => {
    void navigate({
      to: "/web/alerts",
      search: { search: alertsSearchForBucket(x, bucket) },
    });
  };

  const weekdayData: Record<string, number> = useMemo(() => {
    const wd = data?.weekday ?? {};
    const out: Record<string, number> = {};
    for (let i = 0; i < WEEKDAY_LABELS.length; i++) {
      out[WEEKDAY_LABELS[i]!] = wd[WEEKDAY_KEYS[i]!] ?? 0;
    }
    return out;
  }, [data]);

  return (
    <div className={styles.page}>
      {/* Header */}
      <div className={styles.header}>
        <h1 className={styles.title}>Dashboard</h1>
        <TimeRangePicker value={range} onChange={setRange} />
      </div>

      {/* KPI strip */}
      {stats.isPending ? (
        <div className={styles.empty}>
          <Spinner size={20} />
        </div>
      ) : stats.isError ? (
        <div className={styles.empty}>Failed to load dashboard stats.</div>
      ) : data ? (
        <>
          <StatTiles snapshot={data.snapshot} totals={data.totals} />

          {/* Row 1: hero chart + activity feed */}
          <div className={styles.row1}>
            <Card padded>
              <h2 className={styles.cardTitle}>Alerts over time</h2>
              {lineSeries.length === 0 ? (
                <div className={styles.empty}>No data.</div>
              ) : (
                <LineChart
                  series={lineSeries}
                  height={280}
                  toggleableLegend
                  onPointClick={handlePointClick}
                />
              )}
            </Card>
            <Card padded>
              <h2 className={styles.cardTitle}>Recent activity</h2>
              <ActivityFeed />
            </Card>
          </div>

          {/* Row 2: 4 donut/bar panels */}
          <div className={styles.row2}>
            <Card padded>
              <h2 className={styles.cardTitle}>By severity</h2>
              {Object.keys(data.totals.by_severity).length > 0 ? (
                <DonutChart
                  data={data.totals.by_severity}
                  colors={severityColors(Object.keys(data.totals.by_severity))}
                />
              ) : (
                <div className={styles.empty}>No data.</div>
              )}
            </Card>

            <Card padded>
              <h2 className={styles.cardTitle}>By environment</h2>
              {Object.keys(data.totals.by_environment).length > 0 ? (
                <DonutChart data={data.totals.by_environment} />
              ) : (
                <div className={styles.empty}>No data.</div>
              )}
            </Card>

            <Card padded>
              <h2 className={styles.cardTitle}>By state</h2>
              {Object.keys(data.snapshot.by_state).length > 0 ? (
                <DonutChart data={data.snapshot.by_state} />
              ) : (
                <div className={styles.empty}>No data.</div>
              )}
            </Card>

            <Card padded>
              <h2 className={styles.cardTitle}>Top hosts</h2>
              {Object.keys(data.totals.by_host).length > 0 ? (
                <BarChart
                  horizontal
                  series={[{ label: "Hosts", color: "#4f8cff", data: data.totals.by_host }]}
                />
              ) : (
                <div className={styles.empty}>No data.</div>
              )}
            </Card>
          </div>

          {/* Row 3: 4 bar panels */}
          <div className={styles.row3}>
            <Card padded>
              <h2 className={styles.cardTitle}>Actions</h2>
              {Object.keys(data.totals.by_action_success).length > 0 ||
              Object.keys(data.totals.by_action_failure).length > 0 ? (
                <BarChart
                  series={[
                    {
                      label: "Successful",
                      color: "#3fb950",
                      data: data.totals.by_action_success,
                    },
                    {
                      label: "Failed",
                      color: "#f04949",
                      data: data.totals.by_action_failure,
                    },
                  ]}
                />
              ) : (
                <div className={styles.empty}>No data.</div>
              )}
            </Card>

            <Card padded>
              <h2 className={styles.cardTitle}>Throttled by rule</h2>
              {Object.keys(data.totals.by_throttled).length > 0 ? (
                <BarChart
                  series={[
                    { label: "Throttled", color: "#8957e5", data: data.totals.by_throttled },
                  ]}
                />
              ) : (
                <div className={styles.empty}>No data.</div>
              )}
            </Card>

            <Card padded>
              <h2 className={styles.cardTitle}>Snoozed by filter</h2>
              {Object.keys(data.totals.by_snoozed).length > 0 ? (
                <BarChart
                  series={[{ label: "Snoozed", color: "#d4a017", data: data.totals.by_snoozed }]}
                />
              ) : (
                <div className={styles.empty}>No data.</div>
              )}
            </Card>

            <Card padded>
              <h2 className={styles.cardTitle}>By weekday</h2>
              {Object.values(weekdayData).some((v) => v > 0) ? (
                <BarChart series={[{ label: "Alerts", color: "#4f8cff", data: weekdayData }]} />
              ) : (
                <div className={styles.empty}>No data.</div>
              )}
            </Card>
          </div>
        </>
      ) : null}
    </div>
  );
}

function bucketFromRange(range: TimeRange["range"]): number {
  if (range === "1d") return 3600; // 1h buckets (hourly server-side)
  if (range === "1w") return 3600; // 1h buckets
  if (range === "1m") return 21600; // 6h buckets
  if (range === "1y") return 86400; // 1d buckets
  return 3600;
}
