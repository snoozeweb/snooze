import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { DistributionBar, type DistributionDatum } from "./DistributionBar";

const data: DistributionDatum[] = [
  { label: "critical", value: 4, color: "#ff5952" },
  { label: "warning", value: 8, color: "#e3b341" },
  { label: "info", value: 0, color: "#58a6ff" },
];

describe("DistributionBar", () => {
  it("renders one legend row per non-zero datum with count and percent", () => {
    render(<DistributionBar data={data} />);
    // total = 12; critical 4 → 33.3%, warning 8 → 66.7%.
    expect(screen.getByText("critical")).toBeInTheDocument();
    expect(screen.getByText("warning")).toBeInTheDocument();
    expect(screen.getByText("4")).toBeInTheDocument();
    expect(screen.getByText("8")).toBeInTheDocument();
    expect(screen.getByText("33.3%")).toBeInTheDocument();
    expect(screen.getByText("66.7%")).toBeInTheDocument();
    // The zero-value "info" entry is dropped entirely.
    expect(screen.queryByText("info")).not.toBeInTheDocument();
  });

  it("renders nothing when there is no non-zero data", () => {
    const { container } = render(
      <DistributionBar data={[{ label: "x", value: 0, color: "#000" }]} />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("renders plain (non-interactive) segments and rows without onSegmentClick", () => {
    render(<DistributionBar data={data} />);
    expect(screen.queryAllByRole("button")).toHaveLength(0);
  });

  it("makes segments and legend rows clickable when onSegmentClick is set", async () => {
    const onSegmentClick = vi.fn();
    const user = userEvent.setup();
    render(<DistributionBar data={data} onSegmentClick={onSegmentClick} />);
    // 2 segments + 2 legend rows = 4 buttons.
    expect(screen.getAllByRole("button")).toHaveLength(4);

    // Click the warning legend row (matched by its accessible text).
    await user.click(screen.getByText("warning"));
    expect(onSegmentClick).toHaveBeenCalledWith("warning");
  });

  it("sizes segments proportionally to their value", () => {
    const { container } = render(<DistributionBar data={data} />);
    const segments = container.querySelectorAll('[class*="segment"]');
    expect(segments).toHaveLength(2);
    // critical 4/12 ≈ 33.33%, warning 8/12 ≈ 66.67%.
    expect((segments[0] as HTMLElement).style.width).toMatch(/^33\.33/);
    expect((segments[1] as HTMLElement).style.width).toMatch(/^66\.66/);
  });
});
