import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ConfidenceMeter } from "./ConfidenceMeter";

describe("ConfidenceMeter", () => {
  it("fills one segment per level and says the level in words", () => {
    const { container, rerender } = render(<ConfidenceMeter confidence="high" />);
    expect(screen.getByText("High confidence")).toBeInTheDocument();
    expect(container.querySelectorAll("[data-on]")).toHaveLength(3);
    rerender(<ConfidenceMeter confidence="low" />);
    expect(screen.getByText("Low confidence")).toBeInTheDocument();
    expect(container.querySelectorAll("[data-on]")).toHaveLength(1);
  });

  it("borrows no severity tone — trust is a quantity, not an alarm", () => {
    const { container } = render(<ConfidenceMeter confidence="low" />);
    const meter = container.firstElementChild as HTMLElement;
    expect(meter.className).not.toMatch(/critical|warning|ok/);
    expect(meter).not.toHaveAttribute("data-tone");
  });

  it("keeps the word for assistive tech when printed short", () => {
    render(<ConfidenceMeter confidence="medium" wording="short" />);
    expect(screen.getByText("Medium")).toHaveTextContent("Medium confidence");
  });
});
