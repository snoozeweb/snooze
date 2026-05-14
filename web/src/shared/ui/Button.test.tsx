import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { Button } from "./Button";

describe("Button", () => {
  it("renders children as the accessible name", () => {
    render(<Button>Save</Button>);
    expect(screen.getByRole("button", { name: "Save" })).toBeInTheDocument();
  });

  it("defaults type to button (not submit)", () => {
    render(<Button>Click</Button>);
    expect(screen.getByRole("button")).toHaveAttribute("type", "button");
  });

  it("invokes onClick when clicked", async () => {
    const handler = vi.fn();
    const user = userEvent.setup();
    render(<Button onClick={handler}>Hit me</Button>);
    await user.click(screen.getByRole("button"));
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it("renders disabled and does not fire onClick", async () => {
    const handler = vi.fn();
    const user = userEvent.setup();
    render(
      <Button onClick={handler} disabled>
        Nope
      </Button>,
    );
    const btn = screen.getByRole("button");
    expect(btn).toBeDisabled();
    await user.click(btn);
    expect(handler).not.toHaveBeenCalled();
  });

  it("loading=true disables and exposes aria-busy", () => {
    render(<Button loading>Saving</Button>);
    const btn = screen.getByRole("button");
    expect(btn).toBeDisabled();
    expect(btn).toHaveAttribute("aria-busy", "true");
  });

  it("variant prop maps to a class name", () => {
    const { rerender } = render(<Button variant="primary">x</Button>);
    expect(screen.getByRole("button").className).toMatch(/primary/);
    rerender(<Button variant="danger">x</Button>);
    expect(screen.getByRole("button").className).toMatch(/danger/);
  });

  it("size prop maps to a class name", () => {
    const { rerender } = render(<Button size="sm">x</Button>);
    expect(screen.getByRole("button").className).toMatch(/sm/);
    rerender(<Button size="lg">x</Button>);
    expect(screen.getByRole("button").className).toMatch(/lg/);
  });

  it("renders leading and trailing icons when given", () => {
    render(
      <Button leadingIcon="plus" trailingIcon="chevron-down">
        New
      </Button>,
    );
    const btn = screen.getByRole("button");
    expect(btn.querySelectorAll("svg")).toHaveLength(2);
  });
});
