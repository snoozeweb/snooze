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
import styles from "./chart.module.css";

Chart.register(BarController, BarElement, CategoryScale, LinearScale, Tooltip, Legend);

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
};

export function BarChart({ series, height = 240, horizontal = false }: BarChartProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const chartRef = useRef<Chart | null>(null);

  useEffect(() => {
    if (!canvasRef.current) return;
    const labels = uniqueLabels(series);
    const datasets: ChartDataset<"bar">[] = series.map((s) => ({
      label: s.label,
      data: labels.map((l) => s.data[l] ?? 0),
      backgroundColor: s.color,
      borderRadius: 2,
      borderWidth: 0,
    }));
    const options: ChartOptions<"bar"> = {
      responsive: true,
      maintainAspectRatio: false,
      indexAxis: horizontal ? "y" : "x",
      plugins: {
        legend: { position: "bottom", labels: { boxWidth: 10, boxHeight: 10 } },
      },
      scales: {
        x: { grid: { display: false }, ticks: { color: cssVar("--text-muted") } },
        y: {
          grid: { color: cssVar("--border-muted") },
          beginAtZero: true,
          ticks: { color: cssVar("--text-muted") },
        },
      },
    };
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
  }, [series, horizontal]);

  return (
    <div className={styles.wrap} style={{ height }}>
      <canvas ref={canvasRef} />
    </div>
  );
}

function uniqueLabels(series: BarSeries[]): string[] {
  const set = new Set<string>();
  for (const s of series) for (const k of Object.keys(s.data)) set.add(k);
  return [...set].sort();
}

function cssVar(name: string): string {
  if (typeof window === "undefined") return "#999";
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return v || "#999";
}
