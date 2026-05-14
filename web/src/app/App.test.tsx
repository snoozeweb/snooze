import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { App } from "./App";

describe("App", () => {
  beforeEach(() => {
    document.documentElement.removeAttribute("data-theme");
    localStorage.clear();
  });

  afterEach(() => {
    document.documentElement.removeAttribute("data-theme");
    localStorage.clear();
  });

  it("renders the Snooze title", () => {
    render(<App />);
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Snooze");
  });

  it("renders the design-foundation placeholder copy", () => {
    render(<App />);
    expect(screen.getByText(/design foundation/i)).toBeInTheDocument();
  });

  it("shows the current theme in the toggle label", () => {
    document.documentElement.setAttribute("data-theme", "dark");
    render(<App />);
    expect(screen.getByRole("button", { name: /switch to light/i })).toBeInTheDocument();
  });

  it("clicking the toggle flips the theme and persists", async () => {
    document.documentElement.setAttribute("data-theme", "dark");
    const user = userEvent.setup();
    render(<App />);
    await user.click(screen.getByRole("button", { name: /switch to light/i }));
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
    expect(localStorage.getItem("snooze.theme")).toBe("light");
    expect(screen.getByRole("button", { name: /switch to dark/i })).toBeInTheDocument();
  });
});
