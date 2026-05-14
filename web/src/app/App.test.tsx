import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { App } from "./App";

describe("App (primitives demo)", () => {
  beforeEach(() => {
    document.documentElement.removeAttribute("data-theme");
    localStorage.clear();
  });
  afterEach(() => {
    document.documentElement.removeAttribute("data-theme");
    localStorage.clear();
  });

  it("renders the primitives page title", () => {
    render(<App />);
    expect(
      screen.getByRole("heading", { level: 1, name: /Snooze · Primitives/i }),
    ).toBeInTheDocument();
  });

  it("renders every primitive section", () => {
    render(<App />);
    const expected = [
      "Button",
      "IconButton",
      "Badge",
      "Tooltip",
      "Popover",
      "Menu",
      "Dialog",
      "Drawer",
      "Tabs",
      "Toast",
      "Input + Textarea",
      "Switch / Checkbox / Radio",
      "Select",
      "Combobox",
    ];
    for (const title of expected) {
      expect(screen.getByRole("heading", { level: 2, name: title })).toBeInTheDocument();
    }
  });

  it("toast button surfaces a toast", async () => {
    const user = userEvent.setup();
    render(<App />);
    await user.click(screen.getByRole("button", { name: /toast success/i }));
    expect(await screen.findByText(/saved successfully/i)).toBeInTheDocument();
  });

  it("has a working theme toggle in the header", async () => {
    document.documentElement.setAttribute("data-theme", "dark");
    const user = userEvent.setup();
    render(<App />);
    await user.click(screen.getByRole("button", { name: /light/i }));
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
  });
});
