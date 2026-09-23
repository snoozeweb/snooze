import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AutomatableBadge } from "./AutomatableBadge";

describe("AutomatableBadge", () => {
  it("names the capability in words, not just an icon", () => {
    render(<AutomatableBadge />);
    expect(screen.getByText("Automatable")).toBeInTheDocument();
  });

  it("says what the flag actually asserts, on hover", () => {
    render(<AutomatableBadge />);
    expect(screen.getByText("Automatable")).toHaveAttribute(
      "title",
      "Every step is low risk and needs no judgement: safe to run unattended",
    );
  });

  it("stays neutral: the palette's colours belong to severity and confidence", () => {
    // Confidence and risk are gradients whose paint IS the reading. This is a
    // capability, and a third coloured chip beside them would read as a third
    // severity — so the class is the neutral variant, not `ok`/`info`.
    render(<AutomatableBadge />);
    expect(screen.getByText("Automatable").className).toMatch(/neutral/);
  });
});
