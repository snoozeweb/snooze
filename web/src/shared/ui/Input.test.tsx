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
});
