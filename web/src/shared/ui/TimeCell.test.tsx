import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TimeCell } from "./TimeCell";
import { TooltipProvider } from "./Tooltip";

function renderCell(epoch: number | undefined) {
  return render(
    <TooltipProvider>
      <TimeCell epoch={epoch} />
    </TooltipProvider>,
  );
}

describe("TimeCell", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    // Pin "now" to 2026-05-22 14:30 local so trimDate branches are stable.
    vi.setSystemTime(new Date(2026, 4, 22, 14, 30));
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("renders '—' for undefined / 0 epochs", () => {
    const { rerender } = renderCell(undefined);
    expect(screen.getByText("—")).toBeInTheDocument();
    rerender(
      <TooltipProvider>
        <TimeCell epoch={0} />
      </TooltipProvider>,
    );
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("renders a <time> element carrying the ISO dateTime", () => {
    const epoch = Math.floor(new Date(2026, 4, 22, 9, 5).getTime() / 1000);
    const { container } = renderCell(epoch);
    const time = container.querySelector("time");
    expect(time).not.toBeNull();
    expect(time!.getAttribute("dateTime")).toBe(new Date(epoch * 1000).toISOString());
  });

  it("uses trimDate smart text for the visible label", () => {
    const epoch = Math.floor(new Date(2026, 4, 22, 9, 5).getTime() / 1000);
    renderCell(epoch);
    // Same-day → "Today HH:mm".
    expect(screen.getByText(/Today 09:05/)).toBeInTheDocument();
  });

  it("prefixes a relative 'Nm ago' hint when the event is < 1h old", () => {
    const epoch = Math.floor(Date.now() / 1000) - 5 * 60; // 5 minutes ago
    const { container } = renderCell(epoch);
    expect(container.textContent).toMatch(/5m ago/);
  });

  it("omits the relative prefix for events older than an hour", () => {
    const epoch = Math.floor(new Date(2026, 4, 22, 9, 5).getTime() / 1000); // hours ago
    const { container } = renderCell(epoch);
    expect(container.textContent).not.toMatch(/ago/);
  });
});
