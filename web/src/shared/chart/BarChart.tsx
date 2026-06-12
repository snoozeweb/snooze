import { useEffect, useRef } from "react";
import {
  BarController,
  BarElement,
  CategoryScale,
  Chart,
  type ChartDataset,
  type ChartOptions,
  Legend,
  LinearScale,
  Tooltip,
} from "chart.js";
import { applyChartDefaults, chartToken, prefersReducedMotion } from "./theme";
import styles from "./chart.module.css";

Chart.register(BarController, BarElement, CategoryScale, LinearScale, Tooltip, Legend);

// Cap displayed category labels (e.g. long hostnames) to this many chars,
// including the trailing ellipsis. Keeps the y-axis legible on the Top-hosts pane.
const MAX_LABEL_LEN = 22;

export type BarSeries = {
  label: string;
  color: string;
  /** Map from category label to value. */
  data: Record<string, number>;
};

export type BarChartProps = {
  series: BarSeries[];
  height?: number;
  horizontal?: boolean;
  /** Category ordering. "label" (default) = alphabetical; "value" = descending by summed value. */
  sort?: "value" | "label";
  /**
   * Accessible name for the canvas, exposed as aria-label + role="img" so
   * the chart is announced as a single labelled image rather than an
   * unlabelled graphic. Describe what the chart shows (e.g. "Alerts by host").
   */
  ariaLabel: string;
  /**
   * Current theme name. Not read directly — passed only so the render
   * effect re-runs (and re-resolves the token-driven axis/grid colours)
   * when the user toggles light/dark with the chart mounted.
   */
  theme?: string;
};

export function BarChart({
  series,
  height = 240,
  horizontal = false,
  sort = "label",
  ariaLabel,
  theme,
}: BarChartProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const chartRef = useRef<Chart | null>(null);

  useEffect(() => {
    if (!canvasRef.current) return;
    applyChartDefaults();
    const labels = orderedLabels(series, sort);
    const datasets: ChartDataset<"bar">[] = series.map((s) => ({
      label: s.label,
      data: labels.map((l) => s.data[l] ?? 0),
      backgroundColor: s.color,
      borderRadius: 2,
      borderWidth: 0,
    }));

    const ellipsize = (s: string) =>
      s.length > MAX_LABEL_LEN ? s.slice(0, MAX_LABEL_LEN - 1) + "…" : s;
    // Horizontal-only: the Top-hosts pane has the vertical room to show every
    // bar's (ellipsized) label, so force-render them all. Vertical charts keep
    // chart.js' default auto-skip to avoid crowding the x-axis with high-
    // cardinality rule/action names.
    const hCategoryTicks = {
      color: chartToken("--text-muted"),
      autoSkip: false,
      callback(_value: unknown, index: number) {
        return ellipsize(labels[index] ?? "");
      },
    };
    const plainTicks = { color: chartToken("--text-muted") };
    const gridMuted = { color: chartToken("--border-muted") };

    const options: ChartOptions<"bar"> = {
      responsive: true,
      maintainAspectRatio: false,
      indexAxis: horizontal ? "y" : "x",
      plugins: {
        legend: { position: "bottom", labels: { boxWidth: 10, boxHeight: 10 } },
        tooltip: {
          callbacks: {
            // Only override the title when a bar is actually hovered; then show
            // its full (non-ellipsized) category name.
            title: (items) => (items.length > 0 ? (labels[items[0]!.dataIndex] ?? "") : ""),
          },
        },
      },
      scales: {
        x: horizontal
          ? { grid: gridMuted, beginAtZero: true, ticks: plainTicks }
          : { grid: { display: false }, ticks: plainTicks },
        y: horizontal
          ? { grid: { display: false }, ticks: hCategoryTicks }
          : { grid: gridMuted, beginAtZero: true, ticks: plainTicks },
      },
    };

    // The global CSS reduced-motion override can't reach canvas animations,
    // so disable Chart.js' own animation when the user prefers reduced motion.
    if (prefersReducedMotion()) options.animation = false;

    chartRef.current?.destroy();
    chartRef.current = new Chart(canvasRef.current, {
      type: "bar",
      data: { labels, datasets },
      options,
    });
    return () => {
      chartRef.current?.destroy();
      chartRef.current = null;
    };
    // `theme` is intentionally a dep: toggling light/dark must re-resolve the
    // token-driven axis/grid colours even though the series data is unchanged.
  }, [series, horizontal, sort, theme]);

  return (
    <div className={styles.wrap} style={{ height }}>
      <canvas ref={canvasRef} role="img" aria-label={ariaLabel} />
    </div>
  );
}

function orderedLabels(series: BarSeries[], sort: "value" | "label"): string[] {
  const totals = new Map<string, number>();
  for (const s of series) {
    for (const [k, v] of Object.entries(s.data)) totals.set(k, (totals.get(k) ?? 0) + v);
  }
  const labels = [...totals.keys()];
  if (sort === "value") {
    return labels.sort((a, b) => (totals.get(b) ?? 0) - (totals.get(a) ?? 0));
  }
  return labels.sort();
}
