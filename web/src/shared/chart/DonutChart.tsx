import { useEffect, useRef } from "react";
import {
  ArcElement,
  Chart,
  type ChartDataset,
  type ChartOptions,
  DoughnutController,
  Legend,
  Tooltip,
} from "chart.js";
import styles from "./chart.module.css";

Chart.register(DoughnutController, ArcElement, Tooltip, Legend);

export type DonutChartProps = {
  /** Label → value (and the colour palette pairs by category). */
  data: Record<string, number>;
  /** Map from label to colour. Missing labels fall through to a default. */
  colors?: Record<string, string>;
  height?: number;
};

const FALLBACK_PALETTE = [
  "#4f8cff",
  "#3fb950",
  "#d4a017",
  "#ef7e3a",
  "#f04949",
  "#8957e5",
  "#6b7785",
];

export function DonutChart({ data, colors, height = 240 }: DonutChartProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const chartRef = useRef<Chart | null>(null);

  useEffect(() => {
    if (!canvasRef.current) return;
    const labels = Object.keys(data);
    const values = labels.map((l) => data[l] ?? 0);
    const backgroundColor = labels.map(
      (l, i) => colors?.[l] ?? FALLBACK_PALETTE[i % FALLBACK_PALETTE.length]!,
    );
    const dataset: ChartDataset<"doughnut"> = {
      data: values,
      backgroundColor,
      borderWidth: 0,
    };
    const options: ChartOptions<"doughnut"> = {
      responsive: true,
      maintainAspectRatio: false,
      cutout: "55%",
      plugins: {
        legend: { position: "bottom", labels: { boxWidth: 10, boxHeight: 10 } },
      },
    };
    chartRef.current?.destroy();
    chartRef.current = new Chart(canvasRef.current, {
      type: "doughnut",
      data: { labels, datasets: [dataset] },
      options,
    });
    return () => {
      chartRef.current?.destroy();
      chartRef.current = null;
    };
  }, [data, colors]);

  return (
    <div className={styles.wrap} style={{ height }}>
      <canvas ref={canvasRef} />
    </div>
  );
}
