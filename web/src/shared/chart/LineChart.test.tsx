import { fireEvent, render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { LineChart } from "./LineChart";

// Chart.js needs a canvas getContext; jsdom returns null by default.
// Stub it minimally so render() doesn't crash.
beforeAll(() => {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-member-access
  (HTMLCanvasElement.prototype as any).getContext = () => ({
    save: () => undefined,
    restore: () => undefined,
    fillRect: () => undefined,
    clearRect: () => undefined,
    getImageData: () => ({ data: [] }),
    putImageData: () => undefined,
    createImageData: () => ({ data: [] }),
    setTransform: () => undefined,
    drawImage: () => undefined,
    measureText: () => ({ width: 0 }),
    transform: () => undefined,
    rect: () => undefined,
    clip: () => undefined,
    fillText: () => undefined,
    strokeText: () => undefined,
    beginPath: () => undefined,
    closePath: () => undefined,
    stroke: () => undefined,
    fill: () => undefined,
    moveTo: () => undefined,
    lineTo: () => undefined,
    quadraticCurveTo: () => undefined,
    bezierCurveTo: () => undefined,
    arc: () => undefined,
    arcTo: () => undefined,
    translate: () => undefined,
    rotate: () => undefined,
    scale: () => undefined,
    canvas: { width: 100, height: 100 },
  });
});

// Minimal Chart.js mock: records the last config passed to `new Chart(...)`.
// The component imports Chart from "chart.js", so we mock that module.
// We keep Chart.register as a no-op so the module-level call doesn't throw.
//
// Typed as `unknown` to avoid TS control-flow narrowing issues with closure
// writes; callers cast to the expected shape when needed.
const captured: { config: unknown } = { config: null };

vi.mock("chart.js", async (importOriginal) => {
  // Pull in all the real named exports (CategoryScale, Legend, etc.) so that
  // Chart.register() at module level doesn't throw "not registered" errors.
  const original = await importOriginal<Record<string, unknown>>();
  class MockChart {
    // Geometry the drag-to-select code reads. getValueForPixel maps a pixel
    // to a bucket index (px/40 → 0,1,2 for the 3-point fixtures below).
    scales = { x: { getValueForPixel: (px: number) => px / 40 } };
    chartArea = { left: 0, right: 100, top: 0, bottom: 100 };
    constructor(_canvas: unknown, config: unknown) {
      captured.config = config;
    }
    destroy() {
      // no-op
    }
    static register(..._args: unknown[]) {
      // no-op
    }
  }
  return { ...original, Chart: MockChart };
});

// Minimal Chart.js config shape the drag tests reach into.
type ChartOnClick = (
  evt: unknown,
  elements: Array<{ datasetIndex: number; index: number }>,
  chart: unknown,
) => void;
type ChartConfig = { options?: { onClick?: ChartOnClick } };

const THREE_POINTS = [
  {
    label: "Alerts",
    color: "#4f8cff",
    data: [
      { x: "2026-06-01T00:00:00Z", y: 1 },
      { x: "2026-06-01T01:00:00Z", y: 2 },
      { x: "2026-06-01T02:00:00Z", y: 3 },
    ],
  },
];

describe("LineChart", () => {
  it("renders a canvas without crashing", () => {
    const { container } = render(
      <LineChart
        ariaLabel="Alerts over time"
        series={[
          {
            label: "Alerts",
            color: "#4f8cff",
            data: [
              { x: "2026-05-14T00:00:00Z", y: 1 },
              { x: "2026-05-14T01:00:00Z", y: 2 },
            ],
          },
        ]}
      />,
    );
    expect(container.querySelector("canvas")).not.toBeNull();
  });

  it("invokes onPointClick with the series label and x when a point is clicked", () => {
    captured.config = null;
    const onPointClick = vi.fn();
    const series = [
      {
        label: "Alerts",
        color: "#4f8cff",
        data: [{ x: "2026-06-01T00:00:00Z", y: 5 }],
      },
    ];
    render(<LineChart ariaLabel="Alerts over time" series={series} onPointClick={onPointClick} />);

    expect(captured.config).not.toBeNull();
    // captured.config is `unknown`; cast to the minimal Chart.js config shape we care about.
    type ChartOnClick = (
      evt: unknown,
      elements: Array<{ datasetIndex: number; index: number }>,
      chart: unknown,
    ) => void;
    type ChartConfig = { options?: { onClick?: ChartOnClick } };
    const onClick = (captured.config as ChartConfig).options?.onClick;
    expect(onClick).toBeDefined();

    // Simulate a Chart.js click event on dataset 0, point 0
    onClick?.(/* event */ {}, [{ datasetIndex: 0, index: 0 }], /* chart instance */ {});

    expect(onPointClick).toHaveBeenCalledWith("Alerts", "2026-06-01T00:00:00Z");
  });

  it("invokes onRangeSelect with the from/to x of a dragged span", () => {
    const onRangeSelect = vi.fn();
    const { container } = render(
      <LineChart ariaLabel="Alerts over time" series={THREE_POINTS} onRangeSelect={onRangeSelect} />,
    );
    const canvas = container.querySelector("canvas")!;
    // Drag from x=0 (bucket 0) to x=80 (bucket 2); 80px > the click threshold.
    fireEvent.mouseDown(canvas, { clientX: 0, button: 0 });
    fireEvent.mouseMove(window, { clientX: 80 });
    fireEvent.mouseUp(window, { clientX: 80 });

    expect(onRangeSelect).toHaveBeenCalledWith("2026-06-01T00:00:00Z", "2026-06-01T02:00:00Z");
  });

  it("treats a sub-threshold press-release as a click, not a range drag", () => {
    const onRangeSelect = vi.fn();
    const { container } = render(
      <LineChart ariaLabel="Alerts over time" series={THREE_POINTS} onRangeSelect={onRangeSelect} />,
    );
    const canvas = container.querySelector("canvas")!;
    // Move only 2px (< DRAG_THRESHOLD_PX) — this is a click, not a selection.
    fireEvent.mouseDown(canvas, { clientX: 10, button: 0 });
    fireEvent.mouseMove(window, { clientX: 12 });
    fireEvent.mouseUp(window, { clientX: 12 });

    expect(onRangeSelect).not.toHaveBeenCalled();
  });

  it("swallows the click the browser fires immediately after a drag", () => {
    const onPointClick = vi.fn();
    const onRangeSelect = vi.fn();
    captured.config = null;
    const { container } = render(
      <LineChart
        ariaLabel="Alerts over time"
        series={THREE_POINTS}
        onPointClick={onPointClick}
        onRangeSelect={onRangeSelect}
      />,
    );
    const canvas = container.querySelector("canvas")!;
    fireEvent.mouseDown(canvas, { clientX: 0, button: 0 });
    fireEvent.mouseMove(window, { clientX: 80 });
    fireEvent.mouseUp(window, { clientX: 80 });
    expect(onRangeSelect).toHaveBeenCalledTimes(1);

    const onClick = (captured.config as ChartConfig).options?.onClick;
    // The post-drag synthetic click must be ignored (no single-bucket drill).
    onClick?.({}, [{ datasetIndex: 0, index: 0 }], {});
    expect(onPointClick).not.toHaveBeenCalled();
    // A later, genuine click drills normally again.
    onClick?.({}, [{ datasetIndex: 0, index: 0 }], {});
    expect(onPointClick).toHaveBeenCalledTimes(1);
  });
});
