import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Record_ } from "../types";
import { AnalysedDot } from "./AnalysedDot";

/** A record carrying an analysis at the given confidence. */
function analysed(confidence: unknown): Record_ {
  return { uid: "r1", agentic: { root_cause: { summary: "s", confidence } } } as Record_;
}

describe("AnalysedDot", () => {
  it("renders nothing for an alert nobody has analysed", () => {
    const { container } = render(<AnalysedDot record={{ uid: "r1" } as Record_} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("renders nothing when the analysis carries no root cause", () => {
    const { container } = render(
      <AnalysedDot record={{ uid: "r1", agentic: { analysis: { by: "alice" } } } as Record_} />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("says 'Analysed' in words, not only in colour", () => {
    render(<AnalysedDot record={analysed("low")} />);
    const dot = screen.getByRole("img", { name: "Analysed · low confidence" });
    expect(dot).toHaveAttribute("title", "Analysed · low confidence");
  });

  it("takes its tone from the confidence", () => {
    const { rerender } = render(<AnalysedDot record={analysed("high")} />);
    expect(screen.getByRole("img", { name: /high confidence/ })).toHaveAttribute("data-tone", "ok");
    rerender(<AnalysedDot record={analysed("medium")} />);
    expect(screen.getByRole("img", { name: /medium confidence/ })).toHaveAttribute(
      "data-tone",
      "warning",
    );
    rerender(<AnalysedDot record={analysed("low")} />);
    expect(screen.getByRole("img", { name: /low confidence/ })).toHaveAttribute(
      "data-tone",
      "critical",
    );
  });

  it("ignores a confidence outside the three known levels rather than picking a tone for it", () => {
    // `agentic` is an untyped extra field on a dynamic record — a hand-edited
    // document could carry anything, and guessing a colour for it would be a
    // lie in the one place the operator scans fastest.
    const { container } = render(<AnalysedDot record={analysed("certain")} />);
    expect(container).toBeEmptyDOMElement();
  });
});
