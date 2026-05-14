import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Badge } from "./Badge";

describe("Badge", () => {
  it("renders children", () => {
    render(<Badge>critical</Badge>);
    expect(screen.getByText("critical")).toBeInTheDocument();
  });

  it("applies the variant class", () => {
    const { rerender } = render(<Badge variant="critical">x</Badge>);
    expect(screen.getByText("x").className).toMatch(/critical/);
    rerender(<Badge variant="ok">y</Badge>);
    expect(screen.getByText("y").className).toMatch(/ok/);
  });

  it("defaults to neutral", () => {
    render(<Badge>n</Badge>);
    expect(screen.getByText("n").className).toMatch(/neutral/);
  });
});
