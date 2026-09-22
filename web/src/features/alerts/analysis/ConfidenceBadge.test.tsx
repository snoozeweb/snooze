import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { CONFIDENCE_LEVELS, confidenceTone } from "./enums";
import { ConfidenceBadge } from "./ConfidenceBadge";

describe("ConfidenceBadge", () => {
  it("spells the level out — colour is never the only carrier", () => {
    render(<ConfidenceBadge confidence="low" />);
    expect(screen.getByText("Low confidence")).toBeInTheDocument();
  });

  it("labels every level, in the enum's own words", () => {
    const { rerender } = render(<ConfidenceBadge confidence="high" />);
    expect(screen.getByText("High confidence")).toBeInTheDocument();
    rerender(<ConfidenceBadge confidence="medium" />);
    expect(screen.getByText("Medium confidence")).toBeInTheDocument();
    rerender(<ConfidenceBadge confidence="low" />);
    expect(screen.getByText("Low confidence")).toBeInTheDocument();
  });

  it("explains itself on hover", () => {
    render(<ConfidenceBadge confidence="medium" />);
    expect(screen.getByText("Medium confidence")).toHaveAttribute(
      "title",
      "Medium confidence in this root cause",
    );
  });

  it("paints high reassuringly and low critically — the tones enums.ts declares", () => {
    // The badge cannot assert its computed colour under jsdom (tokens don't
    // resolve), so pin the mapping it renders from instead: a future tone
    // change has to come through enums.ts, where risk's inverse mapping lives
    // beside it.
    expect(confidenceTone("high")).toBe("ok");
    expect(confidenceTone("medium")).toBe("warning");
    expect(confidenceTone("low")).toBe("critical");
    // Every declared level renders; none falls through to a blank chip.
    for (const level of CONFIDENCE_LEVELS) {
      const { unmount } = render(<ConfidenceBadge confidence={level} />);
      expect(screen.getByText(new RegExp(`${level}\\s+confidence`, "i"))).toBeInTheDocument();
      unmount();
    }
  });
});
