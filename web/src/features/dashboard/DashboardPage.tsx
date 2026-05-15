import { useMemo, useState } from "react";
import { Card } from "@/shared/ui/Card";
import { Spinner } from "@/shared/ui/Spinner";
import { BarChart } from "@/shared/chart/BarChart";
import { DonutChart } from "@/shared/chart/DonutChart";
import { LineChart, type LineSeries } from "@/shared/chart/LineChart";
import { useStats } from "./api";
import { TimeRangePicker } from "./TimeRangePicker";
import { presetToRange, type TimeRange } from "./time-range";
import styles from "./DashboardPage.module.css";

const SEVERITY_COLORS: Record<string, string> = {
  critical: "#f04949",
  error: "#ef7e3a",
  warning: "#d4a017",
  info: "#4f8cff",
  ok: "#3fb950",
};

export function DashboardPage() {
  const [range, setRange] = useState<TimeRange>(() => {
    const r = presetToRange("1d");
    return { range: "1d", from: r.from, to: r.to };
  });
  const bucket = bucketFromRange(range.range);
  const stats = useStats({ from: range.from, to: range.to, bucket });

  const lineSeries: LineSeries[] = useMemo(() => {
    const series = stats.data?.data?.series;
    if (!Array.isArray(series)) return [];
    const sources = new Set<string>();
    for (const b of series) {
      for (const k of Object.keys(b.counts ?? {})) sources.add(k);
    }
    const palette = ["#4f8cff", "#3fb950", "#d4a017", "#ef7e3a", "#f04949", "#8957e5"];
    return [...sources].map((src, i) => ({
      label: src,
      color: palette[i % palette.length]!,
      data: series.map((b) => ({ x: b.t, y: b.counts?.[src] ?? 0 })),
    }));
  }, [stats.data]);

  const totals = stats.data?.data?.totals;

  return (
    <div className={styles.page}>
      <div className={styles.header}>
        <h1 className={styles.title}>Dashboard</h1>
        <TimeRangePicker value={range} onChange={setRange} />
      </div>

      <Card padded className={styles.full!}>
        <h2 className={styles.cardTitle}>Records over time</h2>
        {stats.isPending ? (
          <div className={styles.empty}>
            <Spinner size={20} />
          </div>
        ) : lineSeries.length === 0 ? (
          <div className={styles.empty}>No data for this range.</div>
        ) : (
          <LineChart series={lineSeries} height={280} />
        )}
      </Card>

      <div className={styles.grid}>
        <Card padded>
          <h2 className={styles.cardTitle}>By severity</h2>
          {totals && Object.keys(totals.by_severity).length > 0 ? (
            <DonutChart data={totals.by_severity} colors={SEVERITY_COLORS} />
          ) : (
            <div className={styles.empty}>No data.</div>
          )}
        </Card>

        <Card padded>
          <h2 className={styles.cardTitle}>By environment</h2>
          {totals && Object.keys(totals.by_environment).length > 0 ? (
            <DonutChart data={totals.by_environment} />
          ) : (
            <div className={styles.empty}>No data.</div>
          )}
        </Card>

        <Card padded className={styles.full!}>
          <h2 className={styles.cardTitle}>Actions</h2>
          {totals &&
          (Object.keys(totals.by_action_success).length > 0 ||
            Object.keys(totals.by_action_failure).length > 0) ? (
            <BarChart
              series={[
                {
                  label: "Successful",
                  color: "#3fb950",
                  data: totals.by_action_success,
                },
                {
                  label: "Failed",
                  color: "#f04949",
                  data: totals.by_action_failure,
                },
              ]}
              height={200}
            />
          ) : (
            <div className={styles.empty}>No data.</div>
          )}
        </Card>
      </div>
    </div>
  );
}

function bucketFromRange(range: TimeRange["range"]): number {
  if (range === "1d") return 600; // 10m buckets
  if (range === "1w") return 3600; // 1h buckets
  if (range === "1m") return 21600; // 6h buckets
  if (range === "1y") return 86400; // 1d buckets
  return 3600;
}
