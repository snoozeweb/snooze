import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { Topbar } from "./Topbar";

describe("Topbar", () => {
  it("renders the brand", () => {
    render(<Topbar onOpenPalette={() => undefined} />);
    expect(screen.getByText("Snooze")).toBeInTheDocument();
  });

  it("renders the breadcrumb when given", () => {
    render(<Topbar breadcrumb="Alerts" onOpenPalette={() => undefined} />);
    expect(screen.getByText("Alerts")).toBeInTheDocument();
  });

  it("calls onOpenPalette when the search button is clicked", async () => {
    const handler = vi.fn();
    const user = userEvent.setup();
    render(<Topbar onOpenPalette={handler} />);
    await user.click(screen.getByRole("button", { name: /open command palette/i }));
    expect(handler).toHaveBeenCalledTimes(1);
  });

  it("toggles the theme on the theme icon button", async () => {
    document.documentElement.setAttribute("data-theme", "dark");
    const user = userEvent.setup();
    render(<Topbar onOpenPalette={() => undefined} />);
    await user.click(screen.getByRole("button", { name: /switch to light theme/i }));
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
  });
});
