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

// Pixels the cursor must travel after mousedown before we treat the gesture as
// a range drag rather than a click. Keeps a slightly-jittery click from
// drawing a (zero-width) selection and swallowing the point-click drill-down.
const DRAG_THRESHOLD_PX = 4;

export type LineChartProps = {
  series: LineSeries[];
  height?: number;
  /** Called when a data point is clicked, with the series label and point's x value. */
  onPointClick?: (seriesLabel: string, x: string) => void;
  /**
   * Called when the user drags a range across the plot, with the x values of
   * the first and last buckets the selection covers (Grafana-style). When set,
   * dragging paints a translucent selection box and fires this on release; a
   * plain click still goes through onPointClick. fromX may equal toX when the
   * drag stays within one bucket.
   */
  onRangeSelect?: (fromX: string, toX: string) => void;
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
  onRangeSelect,
  toggleableLegend,
  ariaLabel,
  theme,
}: LineChartProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const chartRef = useRef<Chart | null>(null);
  const overlayRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!canvasRef.current) return;
    // Set by a completed drag so the click the browser fires right after the
    // mouseup doesn't also drill into a single bucket via onClick below.
    let suppressNextClick = false;
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
          if (suppressNextClick) {
            suppressNextClick = false;
            return;
          }
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

    // Grafana-style drag-to-select-range, wired only when the caller wants it.
    // We paint a DOM overlay (not a canvas rect) so it re-themes via CSS tokens
    // and survives Chart.js' own redraws.
    let detachDrag: (() => void) | undefined;
    const canvas = canvasRef.current;
    if (onRangeSelect && canvas) {
      const pxOf = (e: MouseEvent) => e.clientX - canvas.getBoundingClientRect().left;

      // Map a canvas-relative pixel to a bucket index via the x scale.
      const indexAt = (px: number): number | null => {
        const scale = chartRef.current?.scales["x"] as
          | { getValueForPixel?: (p: number) => number | undefined }
          | undefined;
        const data = series[0]?.data;
        if (!scale?.getValueForPixel || !data?.length) return null;
        const raw = scale.getValueForPixel(px);
        if (raw == null || Number.isNaN(raw)) return null;
        return Math.max(0, Math.min(data.length - 1, Math.round(raw)));
      };

      const paint = (aPx: number, bPx: number) => {
        const overlay = overlayRef.current;
        const area = chartRef.current?.chartArea as
          | { left: number; right: number; top: number; bottom: number }
          | undefined;
        if (!overlay || !area) return;
        const left = Math.max(area.left, Math.min(aPx, bPx));
        const right = Math.min(area.right, Math.max(aPx, bPx));
        overlay.style.display = "block";
        overlay.style.left = `${left}px`;
        overlay.style.top = `${area.top}px`;
        overlay.style.width = `${Math.max(0, right - left)}px`;
        overlay.style.height = `${Math.max(0, area.bottom - area.top)}px`;
      };
      const hide = () => {
        if (overlayRef.current) overlayRef.current.style.display = "none";
      };

      let drag: { startPx: number; moved: boolean } | null = null;

      const onMove = (e: MouseEvent) => {
        if (!drag) return;
        const px = pxOf(e);
        if (Math.abs(px - drag.startPx) > DRAG_THRESHOLD_PX) drag.moved = true;
        if (drag.moved) paint(drag.startPx, px);
      };
      const onUp = (e: MouseEvent) => {
        window.removeEventListener("mousemove", onMove);
        window.removeEventListener("mouseup", onUp);
        const d = drag;
        drag = null;
        hide();
        if (!d?.moved) return; // a click, not a drag — let onClick drill instead
        const a = indexAt(d.startPx);
        const b = indexAt(pxOf(e));
        if (a == null || b == null) return;
        const data = series[0]!.data;
        const fromX = data[Math.min(a, b)]?.x;
        const toX = data[Math.max(a, b)]?.x;
        if (fromX == null || toX == null) return;
        suppressNextClick = true; // swallow the click the browser fires post-drag
        onRangeSelect(fromX, toX);
      };
      const onDown = (e: MouseEvent) => {
        if (e.button !== 0) return; // primary button only
        e.preventDefault(); // no text/image drag-select while selecting
        drag = { startPx: pxOf(e), moved: false };
        window.addEventListener("mousemove", onMove);
        window.addEventListener("mouseup", onUp);
      };

      canvas.addEventListener("mousedown", onDown);
      detachDrag = () => {
        canvas.removeEventListener("mousedown", onDown);
        window.removeEventListener("mousemove", onMove);
        window.removeEventListener("mouseup", onUp);
      };
    }

    return () => {
      detachDrag?.();
      chartRef.current?.destroy();
      chartRef.current = null;
    };
    // `theme` is intentionally a dep: toggling light/dark must re-resolve the
    // token-driven axis/grid colours even though the series data is unchanged.
  }, [series, onPointClick, onRangeSelect, toggleableLegend, theme]);

  return (
    <div className={styles.wrap} style={{ height }}>
      <canvas ref={canvasRef} role="img" aria-label={ariaLabel} />
      {onRangeSelect ? (
        <div ref={overlayRef} className={styles.selection} aria-hidden="true" />
      ) : null}
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
