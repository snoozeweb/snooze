import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { Switch } from "./Switch";

describe("Switch", () => {
  it("renders as a switch with the given checked state", () => {
    render(<Switch checked={true} aria-label="Auto-refresh" />);
    expect(screen.getByRole("switch", { name: "Auto-refresh" })).toBeChecked();
  });

  it("invokes onCheckedChange on click", async () => {
    const handler = vi.fn();
    const user = userEvent.setup();
    render(<Switch checked={false} onCheckedChange={handler} aria-label="x" />);
    await user.click(screen.getByRole("switch"));
    expect(handler).toHaveBeenCalledWith(true);
  });
});
