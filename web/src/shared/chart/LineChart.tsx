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
};

export function LineChart({ series, height = 240, onPointClick, toggleableLegend }: LineChartProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const chartRef = useRef<Chart | null>(null);

  useEffect(() => {
    if (!canvasRef.current) return;
    const datasets: ChartDataset<"line">[] = series.map((s) => ({
      label: s.label,
      // Chart.js accepts {x: string, y: number} when using category scale; cast
      // needed because the TS overload expects x: number (scatter/time scale).
      data: s.data as unknown as ChartDataset<"line">["data"],
      borderColor: s.color,
      backgroundColor: hexToRgba(s.color, 0.18),
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
          ticks: { color: cssVar("--text-muted"), maxRotation: 0 },
        },
        y: {
          grid: { color: cssVar("--border-muted") },
          ticks: { color: cssVar("--text-muted") },
          beginAtZero: true,
        },
      },
      ...(onPointClick != null && {
        onClick(
          _evt: unknown,
          elements: Array<{ datasetIndex: number; index: number }>,
        ) {
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
  }, [series, onPointClick, toggleableLegend]);

  return (
    <div className={styles.wrap} style={{ height }}>
      <canvas ref={canvasRef} />
    </div>
  );
}

function hexToRgba(hex: string, alpha: number): string {
  const trimmed = hex.replace("#", "");
  const full =
    trimmed.length === 3
      ? trimmed
          .split("")
          .map((c) => c + c)
          .join("")
      : trimmed;
  const r = parseInt(full.slice(0, 2), 16);
  const g = parseInt(full.slice(2, 4), 16);
  const b = parseInt(full.slice(4, 6), 16);
  return `rgba(${r}, ${g}, ${b}, ${alpha})`;
}

function cssVar(name: string): string {
  if (typeof window === "undefined") return "#999";
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return v || "#999";
}
