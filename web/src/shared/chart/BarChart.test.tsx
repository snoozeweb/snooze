import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { BarChart } from "./BarChart";

beforeAll(() => {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-member-access
  (HTMLCanvasElement.prototype as any).getContext = () => ({
    measureText: () => ({ width: 0 }),
    canvas: { width: 100, height: 100 },
  });
});

const captured: { config: unknown } = { config: null };

vi.mock("chart.js", async (importOriginal) => {
  const original = await importOriginal<Record<string, unknown>>();
  class MockChart {
    constructor(_canvas: unknown, config: unknown) {
      captured.config = config;
    }
    destroy() {}
    static register(..._args: unknown[]) {}
  }
  return { ...original, Chart: MockChart };
});

type Cfg = {
  data: { labels: string[] };
  options: { scales: { x: { ticks?: { autoSkip?: boolean } }; y: { ticks?: { autoSkip?: boolean } } } };
};

describe("BarChart", () => {
  it("keeps categories in alphabetical order by default", () => {
    captured.config = null;
    render(<BarChart series={[{ label: "Hosts", color: "#000", data: { b: 9, a: 1, c: 5 } }]} />);
    expect((captured.config as Cfg).data.labels).toEqual(["a", "b", "c"]);
  });

  it("orders categories by descending value when sort='value'", () => {
    captured.config = null;
    render(<BarChart sort="value" series={[{ label: "Hosts", color: "#000", data: { b: 9, a: 1, c: 5 } }]} />);
    expect((captured.config as Cfg).data.labels).toEqual(["b", "c", "a"]);
  });

  it("disables auto-skip on the category axis for horizontal charts", () => {
    captured.config = null;
    render(<BarChart horizontal series={[{ label: "Hosts", color: "#000", data: { a: 1, b: 2 } }]} />);
    // horizontal → category axis is y
    expect((captured.config as Cfg).options.scales.y.ticks?.autoSkip).toBe(false);
  });

  it("does not force auto-skip on either axis for vertical charts", () => {
    captured.config = null;
    render(<BarChart series={[{ label: "Hosts", color: "#000", data: { a: 1, b: 2 } }]} />);
    // autoSkip:false is reserved for the horizontal (Top-hosts) category axis;
    // vertical charts keep chart.js' default auto-skip on both axes.
    expect((captured.config as Cfg).options.scales.x.ticks?.autoSkip).toBeUndefined();
    expect((captured.config as Cfg).options.scales.y.ticks?.autoSkip).toBeUndefined();
  });

  it("tooltip title shows the full label at the hovered index, and nothing when empty", () => {
    captured.config = null;
    render(<BarChart series={[{ label: "Hosts", color: "#000", data: { alpha: 9, beta: 1 } }]} />);
    type TooltipCfg = {
      options: {
        plugins: { tooltip: { callbacks: { title: (items: Array<{ dataIndex: number }>) => string } } };
      };
    };
    const title = (captured.config as TooltipCfg).options.plugins.tooltip.callbacks.title;
    expect(title([{ dataIndex: 0 }])).toBe("alpha");
    expect(title([{ dataIndex: 1 }])).toBe("beta");
    expect(title([])).toBe("");
  });
});
