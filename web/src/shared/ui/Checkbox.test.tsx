import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { Checkbox } from "./Checkbox";

describe("Checkbox", () => {
  it("toggles on click", async () => {
    const onCheckedChange = vi.fn();
    const user = userEvent.setup();
    render(<Checkbox aria-label="Select" onCheckedChange={onCheckedChange} />);
    await user.click(screen.getByRole("checkbox"));
    expect(onCheckedChange).toHaveBeenCalledWith(true);
  });

  it("reads as checked when defaultChecked", () => {
    render(<Checkbox aria-label="x" defaultChecked />);
    expect(screen.getByRole("checkbox")).toBeChecked();
  });
});
