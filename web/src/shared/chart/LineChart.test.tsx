import { render } from "@testing-library/react";
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

describe("LineChart", () => {
  it("renders a canvas without crashing", () => {
    const { container } = render(
      <LineChart
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
    render(<LineChart series={series} onPointClick={onPointClick} />);

    expect(captured.config).not.toBeNull();
    // captured.config is `unknown`; we must cast to reach Chart.js options.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-member-access
    const onClick = ((captured.config as any).options as {
      onClick?: (
        evt: unknown,
        elements: Array<{ datasetIndex: number; index: number }>,
        chart: unknown,
      ) => void;
    } | undefined)?.onClick;
    expect(onClick).toBeDefined();

    // Simulate a Chart.js click event on dataset 0, point 0
    onClick?.(
      /* event */ {},
      [{ datasetIndex: 0, index: 0 }],
      /* chart instance */ {},
    );

    expect(onPointClick).toHaveBeenCalledWith("Alerts", "2026-06-01T00:00:00Z");
  });
});
