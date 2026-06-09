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

  it("supports the closed and platform variants", () => {
    const { rerender } = render(<Badge variant="closed">c</Badge>);
    expect(screen.getByText("c").className).toMatch(/closed/);
    rerender(<Badge variant="platform">p</Badge>);
    expect(screen.getByText("p").className).toMatch(/platform/);
  });

  it("applies a custom colour via inline style, dropping the variant class", () => {
    render(
      <Badge variant="critical" color="#f04949">
        sev
      </Badge>,
    );
    const el = screen.getByText("sev");
    // color wins over variant: the critical class is gone, the hex drives the
    // text/border colour and a 15%-alpha fill (the browser serialises the hex
    // to rgb()/rgba()).
    expect(el.className).not.toMatch(/critical/);
    expect(el.style.color).toBe("rgb(240, 73, 73)");
    expect(el.style.background).toBe("rgba(240, 73, 73, 0.15)");
  });
});
