import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { IconButton } from "./IconButton";
import { TooltipProvider } from "./Tooltip";

describe("IconButton", () => {
  it("uses label as accessible name and drops the native title (app Tooltip instead)", () => {
    render(<IconButton icon="refresh" label="Refresh" />);
    const btn = screen.getByRole("button", { name: "Refresh" });
    // The native `title` bubble is replaced by the styled app Tooltip so hover
    // hints match the rest of the mission-control UI.
    expect(btn).not.toHaveAttribute("title");
  });

  it("shows the styled app Tooltip on hover", async () => {
    const user = userEvent.setup();
    render(
      <TooltipProvider delay={0}>
        <IconButton icon="refresh" label="Refresh the list" />
      </TooltipProvider>,
    );
    expect(screen.queryByRole("tooltip")).toBeNull();
    await user.hover(screen.getByRole("button"));
    expect(await screen.findByRole("tooltip")).toHaveTextContent("Refresh the list");
  });

  it("keeps a native title as a fallback on a disabled button (Radix Tooltip can't fire when disabled)", () => {
    render(<IconButton icon="x" label="Close" disabled />);
    // A disabled button emits no pointer/focus events, so the styled Tooltip
    // never opens — the native title is the only hover hint left, so we keep it.
    expect(screen.getByRole("button", { name: "Close" })).toHaveAttribute("title", "Close");
  });

  it("renders without a TooltipProvider without throwing (bare test harnesses)", () => {
    // Most unit tests render an IconButton with no TooltipProvider ancestor.
    // Radix Tooltip must degrade gracefully rather than crash those suites.
    render(<IconButton icon="bell" label="Bell" />);
    expect(screen.getByRole("button", { name: "Bell" })).toBeInTheDocument();
  });

  it("withTooltip={false} renders a bare button so it can compose as a Radix trigger", async () => {
    const user = userEvent.setup();
    render(
      <TooltipProvider delay={0}>
        <IconButton icon="more-horizontal" label="Row actions" withTooltip={false} />
      </TooltipProvider>,
    );
    await user.hover(screen.getByRole("button", { name: "Row actions" }));
    // No self-rendered tooltip: the parent trigger composes hover behaviour.
    expect(screen.queryByRole("tooltip")).toBeNull();
  });

  it("renders the chosen icon glyph", () => {
    const { container } = render(<IconButton icon="bell" label="Bell" />);
    expect(container.querySelector('use[href$="#icon-bell"]')).not.toBeNull();
  });

  it("disabled prop is honoured", () => {
    render(<IconButton icon="x" label="Close" disabled />);
    expect(screen.getByRole("button")).toBeDisabled();
  });

  it("size prop maps to a class", () => {
    render(<IconButton icon="x" label="Close" size="lg" />);
    expect(screen.getByRole("button").className).toMatch(/lg/);
  });
});
