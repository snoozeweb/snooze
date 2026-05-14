import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Textarea } from "./Textarea";

describe("Textarea", () => {
  it("renders with placeholder and rows", () => {
    render(<Textarea placeholder="Comment…" rows={5} />);
    const el = screen.getByPlaceholderText("Comment…");
    expect(el.tagName).toBe("TEXTAREA");
    expect(el).toHaveAttribute("rows", "5");
  });

  it("invalid prop sets aria-invalid", () => {
    render(<Textarea invalid />);
    expect(screen.getByRole("textbox")).toHaveAttribute("aria-invalid", "true");
  });
});
