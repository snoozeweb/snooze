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

  it("renders each primitive section", () => {
    render(<App />);
    const expected = [
      "Button",
      "IconButton",
      "Badge",
      "Spinner / Skeleton",
      "Card",
      "Kbd",
      "Code / CodeBlock",
      "EmptyState",
    ];
    for (const title of expected) {
      expect(screen.getByRole("heading", { level: 2, name: title })).toBeInTheDocument();
    }
  });

  it("has a working theme toggle in the header", async () => {
    document.documentElement.setAttribute("data-theme", "dark");
    const user = userEvent.setup();
    render(<App />);
    // Theme toggle label reflects the OPPOSITE theme (i.e., "Light" in dark mode).
    await user.click(screen.getByRole("button", { name: /light/i }));
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
  });
});
