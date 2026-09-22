import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { RISK_LEVELS, riskTone } from "./enums";
import { RiskBadge } from "./RiskBadge";

describe("RiskBadge", () => {
  it("spells the level out — colour is never the only carrier", () => {
    render(<RiskBadge risk="high" />);
    expect(screen.getByText("High risk")).toBeInTheDocument();
  });

  it("labels every level", () => {
    const { rerender } = render(<RiskBadge risk="low" />);
    expect(screen.getByText("Low risk")).toBeInTheDocument();
    rerender(<RiskBadge risk="medium" />);
    expect(screen.getByText("Medium risk")).toBeInTheDocument();
    rerender(<RiskBadge risk="high" />);
    expect(screen.getByText("High risk")).toBeInTheDocument();
  });

  it("explains itself on hover", () => {
    render(<RiskBadge risk="low" />);
    expect(screen.getByText("Low risk")).toHaveAttribute("title", "Low risk if this step is run");
  });

  it("inverts confidence's tone mapping — a high-risk step is never painted green", () => {
    expect(riskTone("low")).toBe("ok");
    expect(riskTone("medium")).toBe("warning");
    expect(riskTone("high")).toBe("critical");
    for (const level of RISK_LEVELS) {
      const { unmount } = render(<RiskBadge risk={level} />);
      expect(screen.getByText(new RegExp(`${level}\\s+risk`, "i"))).toBeInTheDocument();
      unmount();
    }
  });
});
