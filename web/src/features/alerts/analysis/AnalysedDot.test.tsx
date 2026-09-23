import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Record_ } from "../types";
import { AnalysedDot } from "./AnalysedDot";

/** A record carrying an analysis at the given confidence (and plan verdict). */
function analysed(confidence: unknown, status?: unknown): Record_ {
  return {
    uid: "r1",
    agentic: {
      root_cause: { summary: "s", confidence },
      ...(status !== undefined ? { remediation_plan: { status, steps: [] } } : {}),
    },
  } as Record_;
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

  it("takes its tone from the verdict, never from the confidence", () => {
    // Confidence used to paint the dot green/amber/red, so a low-confidence
    // cause read as a second Critical beside the severity badge. The dot now
    // says what the alert needs: amber for "act", green for "recovered".
    const { rerender } = render(<AnalysedDot record={analysed("low", "action_required")} />);
    expect(screen.getByRole("img", { name: /low confidence/ })).toHaveAttribute(
      "data-tone",
      "warning",
    );
    rerender(<AnalysedDot record={analysed("high", "self_resolved")} />);
    expect(screen.getByRole("img", { name: /high confidence/ })).toHaveAttribute("data-tone", "ok");
    rerender(<AnalysedDot record={analysed("high", "monitoring")} />);
    expect(screen.getByRole("img", { name: /high confidence/ })).toHaveAttribute(
      "data-tone",
      "neutral",
    );
    // No verdict stated: neutral whatever the confidence.
    rerender(<AnalysedDot record={analysed("low")} />);
    expect(screen.getByRole("img", { name: /low confidence/ })).toHaveAttribute(
      "data-tone",
      "neutral",
    );
  });

  it("names the verdict in words when the plan states one", () => {
    render(<AnalysedDot record={analysed("medium", "action_required")} />);
    expect(
      screen.getByRole("img", { name: "Analysed · medium confidence · Action required" }),
    ).toBeInTheDocument();
  });

  it("ignores a verdict outside the three known values", () => {
    render(<AnalysedDot record={analysed("high", "panic")} />);
    const dot = screen.getByRole("img", { name: "Analysed · high confidence" });
    expect(dot).toHaveAttribute("data-tone", "neutral");
  });

  it("ignores a confidence outside the three known levels rather than picking a tone for it", () => {
    // `agentic` is an untyped extra field on a dynamic record — a hand-edited
    // document could carry anything, and guessing a colour for it would be a
    // lie in the one place the operator scans fastest.
    const { container } = render(<AnalysedDot record={analysed("certain")} />);
    expect(container).toBeEmptyDOMElement();
  });
});
