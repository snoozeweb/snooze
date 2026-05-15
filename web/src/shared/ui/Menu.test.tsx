import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { Menu, MenuContent, MenuItem, MenuSeparator, MenuTrigger } from "./Menu";

describe("Menu", () => {
  it("opens on trigger click and shows items", async () => {
    const user = userEvent.setup();
    render(
      <Menu>
        <MenuTrigger>
          <button type="button">open</button>
        </MenuTrigger>
        <MenuContent>
          <MenuItem>Edit</MenuItem>
          <MenuSeparator />
          <MenuItem>Delete</MenuItem>
        </MenuContent>
      </Menu>,
    );
    await user.click(screen.getByText("open"));
    expect(screen.getByRole("menuitem", { name: "Edit" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Delete" })).toBeInTheDocument();
  });

  it("invokes item onSelect when clicked", async () => {
    const handler = vi.fn();
    const user = userEvent.setup();
    render(
      <Menu>
        <MenuTrigger>
          <button type="button">open</button>
        </MenuTrigger>
        <MenuContent>
          <MenuItem onSelect={handler}>Run</MenuItem>
        </MenuContent>
      </Menu>,
    );
    await user.click(screen.getByText("open"));
    await user.click(screen.getByRole("menuitem", { name: "Run" }));
    expect(handler).toHaveBeenCalledTimes(1);
  });
});
