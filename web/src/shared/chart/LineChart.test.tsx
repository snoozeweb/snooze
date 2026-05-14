import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
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
});
