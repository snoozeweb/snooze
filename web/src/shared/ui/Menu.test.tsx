import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { Menu, MenuContent, MenuItem, MenuLabel, MenuSeparator, MenuTrigger } from "./Menu";

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

  it("renders an item's description as a second line and keeps it in the item's text", async () => {
    const user = userEvent.setup();
    render(
      <Menu>
        <MenuTrigger>
          <button type="button">open</button>
        </MenuTrigger>
        <MenuContent>
          <MenuItem description="Mark it resolved.">Close</MenuItem>
        </MenuContent>
      </Menu>,
    );
    await user.click(screen.getByText("open"));
    expect(screen.getByText("Mark it resolved.")).toBeInTheDocument();
    // The description lives inside the item, so assistive tech reads it with
    // the item rather than requiring a hover — hence the name is the label
    // *plus* the description, not the label alone.
    expect(
      screen.getByRole("menuitem", { name: /close\s*mark it resolved\./i }),
    ).toBeInTheDocument();
  });

  it("renders a MenuLabel heading that is not a menu item", async () => {
    const user = userEvent.setup();
    render(
      <Menu>
        <MenuTrigger>
          <button type="button">open</button>
        </MenuTrigger>
        <MenuContent>
          <MenuLabel>Quiet it down</MenuLabel>
          <MenuItem>Shelve</MenuItem>
        </MenuContent>
      </Menu>,
    );
    await user.click(screen.getByText("open"));
    expect(screen.getByText("Quiet it down")).toBeInTheDocument();
    // A heading must not become a keyboard stop.
    expect(screen.getAllByRole("menuitem")).toHaveLength(1);
  });
});
