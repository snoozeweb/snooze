import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Card } from "./Card";

describe("Card", () => {
  it("renders its children inside a section", () => {
    render(<Card>hello</Card>);
    expect(screen.getByText("hello").tagName).toBe("SECTION");
  });

  it("elevated adds a class", () => {
    render(<Card elevated>x</Card>);
    expect(screen.getByText("x").className).toMatch(/elevated/);
  });

  it("padded adds a class", () => {
    render(<Card padded>x</Card>);
    expect(screen.getByText("x").className).toMatch(/padded/);
  });
});
