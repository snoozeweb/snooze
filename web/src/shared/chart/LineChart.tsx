import { useEffect, useRef } from "react";
import {
  Chart,
  type ChartDataset,
  type ChartOptions,
  CategoryScale,
  Filler,
  Legend,
  LinearScale,
  LineController,
  LineElement,
  PointElement,
  TimeScale,
  Tooltip,
} from "chart.js";
import { applyChartDefaults, chartToken, prefersReducedMotion } from "./theme";
import styles from "./chart.module.css";

Chart.register(
  LineController,
  LineElement,
  PointElement,
  LinearScale,
  CategoryScale,
  TimeScale,
  Tooltip,
  Legend,
  Filler,
);

export type LineSeries = {
  label: string;
  color: string;
  /** Pairs of [ISO time, value]. */
  data: Array<{ x: string; y: number }>;
};

export type LineChartProps = {
  series: LineSeries[];
  height?: number;
  /** Called when a data point is clicked, with the series label and point's x value. */
  onPointClick?: (seriesLabel: string, x: string) => void;
  /** When true, the Chart.js built-in legend is shown and supports click-toggling datasets. */
  toggleableLegend?: boolean;
  /**
   * Accessible name for the canvas, exposed as aria-label + role="img" so
   * the chart is announced as a single labelled image rather than an
   * unlabelled graphic. Describe what the chart shows (e.g. "Alerts over time").
   */
  ariaLabel: string;
  /**
   * Current theme name. Not read directly — passed only so the render
   * effect re-runs (and re-resolves the token-driven axis/grid colours)
   * when the user toggles light/dark with the chart mounted.
   */
  theme?: string;
};

export function LineChart({
  series,
  height = 240,
  onPointClick,
  toggleableLegend,
  ariaLabel,
  theme,
}: LineChartProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const chartRef = useRef<Chart | null>(null);

  useEffect(() => {
    if (!canvasRef.current) return;
    applyChartDefaults();
    const datasets: ChartDataset<"line">[] = series.map((s) => ({
      label: s.label,
      // Chart.js accepts {x: string, y: number} when using category scale; cast
      // needed because the TS overload expects x: number (scatter/time scale).
      data: s.data as unknown as ChartDataset<"line">["data"],
      borderColor: s.color,
      backgroundColor: toRgba(s.color, 0.18),
      fill: true,
      pointRadius: 0,
      tension: 0.3,
      borderWidth: 1.5,
    }));
    const options: ChartOptions<"line"> = {
      responsive: true,
      maintainAspectRatio: false,
      interaction: { intersect: false, mode: "index" },
      plugins: {
        legend: {
          display: toggleableLegend === true ? true : undefined,
          position: "bottom",
          labels: { boxWidth: 10, boxHeight: 10 },
        },
        tooltip: { mode: "index", intersect: false },
      },
      scales: {
        x: {
          type: "category",
          grid: { display: false },
          ticks: { color: chartToken("--text-muted"), maxRotation: 0 },
        },
        y: {
          grid: { color: chartToken("--border-muted") },
          ticks: { color: chartToken("--text-muted") },
          beginAtZero: true,
        },
      },
      ...(onPointClick != null && {
        onClick(_evt: unknown, elements: Array<{ datasetIndex: number; index: number }>) {
          if (!elements.length) return;
          const { datasetIndex, index } = elements[0]!;
          const seriesLabel = series[datasetIndex]?.label;
          const x = series[datasetIndex]?.data[index]?.x;
          if (seriesLabel != null && x != null) {
            onPointClick(seriesLabel, x);
          }
        },
      }),
    };
    // The global CSS reduced-motion override can't reach canvas animations,
    // so disable Chart.js' own animation when the user prefers reduced motion.
    if (prefersReducedMotion()) options.animation = false;
    chartRef.current?.destroy();
    chartRef.current = new Chart(canvasRef.current, {
      type: "line",
      data: { datasets },
      options,
    });
    return () => {
      chartRef.current?.destroy();
      chartRef.current = null;
    };
    // `theme` is intentionally a dep: toggling light/dark must re-resolve the
    // token-driven axis/grid colours even though the series data is unchanged.
  }, [series, onPointClick, toggleableLegend, theme]);

  return (
    <div className={styles.wrap} style={{ height }}>
      <canvas ref={canvasRef} role="img" aria-label={ariaLabel} />
    </div>
  );
}

// Build a translucent fill colour from a series colour. Handles 3/6-digit
// hex (the common case) and falls back to color-mix for any other CSS
// colour string the caller might pass (e.g. a resolved rgb()).
function toRgba(color: string, alpha: number): string {
  const hex = color.trim();
  const m = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(hex);
  if (!m) return `color-mix(in srgb, ${color} ${Math.round(alpha * 100)}%, transparent)`;
  const full =
    m[1]!.length === 3
      ? m[1]!
          .split("")
          .map((c) => c + c)
          .join("")
      : m[1]!;
  const r = parseInt(full.slice(0, 2), 16);
  const g = parseInt(full.slice(2, 4), 16);
  const b = parseInt(full.slice(4, 6), 16);
  return `rgba(${r}, ${g}, ${b}, ${alpha})`;
}
