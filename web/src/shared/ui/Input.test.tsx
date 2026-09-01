import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { Input } from "./Input";

describe("Input", () => {
  it("renders with placeholder", () => {
    render(<Input placeholder="Search alerts…" />);
    expect(screen.getByPlaceholderText("Search alerts…")).toBeInTheDocument();
  });

  it("invokes onChange", async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    render(<Input onChange={onChange} />);
    await user.type(screen.getByRole("textbox"), "hi");
    expect(onChange).toHaveBeenCalled();
  });

  it("invalid prop sets aria-invalid", () => {
    render(<Input invalid />);
    expect(screen.getByRole("textbox")).toHaveAttribute("aria-invalid", "true");
  });

  it("renders leading + trailing icons", () => {
    const { container } = render(<Input leadingIcon="search" trailingIcon="x" />);
    expect(container.querySelectorAll("svg")).toHaveLength(2);
  });

  it("visually reads as disabled (like a disabled Button) when disabled", () => {
    const { container } = render(<Input disabled aria-label="Tenant ID" />);
    expect(screen.getByRole("textbox")).toBeDisabled();
    // The wrapper (not just the raw input) carries a disabled class so it dims
    // and drops the hover affordance, matching disabled Buttons.
    const wrap = container.querySelector("div");
    expect(wrap?.className).toMatch(/disabled/i);
  });

  it("pairs invalid + errorMessage with a visible, announced message wired via aria-describedby", () => {
    render(<Input invalid errorMessage="Name is required." aria-label="Name" />);
    const input = screen.getByRole("textbox");
    const message = screen.getByRole("alert");
    expect(message).toHaveTextContent("Name is required.");
    expect(input.getAttribute("aria-describedby")).toBe(message.id);
  });

  it("does not render a message when invalid but no errorMessage is given", () => {
    render(<Input invalid aria-label="Name" />);
    expect(screen.queryByRole("alert")).toBeNull();
  });
});
