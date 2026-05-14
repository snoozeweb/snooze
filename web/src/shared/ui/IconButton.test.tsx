import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { IconButton } from "./IconButton";

describe("IconButton", () => {
  it("uses label as accessible name and title", () => {
    render(<IconButton icon="refresh" label="Refresh" />);
    const btn = screen.getByRole("button", { name: "Refresh" });
    expect(btn).toHaveAttribute("title", "Refresh");
  });

  it("renders the chosen icon glyph", () => {
    const { container } = render(<IconButton icon="bell" label="Bell" />);
    expect(container.querySelector('use[href$="#icon-bell"]')).not.toBeNull();
  });

  it("disabled prop is honoured", () => {
    render(<IconButton icon="x" label="Close" disabled />);
    expect(screen.getByRole("button")).toBeDisabled();
  });

  it("size prop maps to a class", () => {
    render(<IconButton icon="x" label="Close" size="lg" />);
    expect(screen.getByRole("button").className).toMatch(/lg/);
  });
});
