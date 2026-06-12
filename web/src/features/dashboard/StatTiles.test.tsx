import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import type { StatsTotals } from "./types";
import { StatTiles } from "./StatTiles";

const totals: StatsTotals = {
  by_severity: {},
  by_environment: {},
  by_host: {},
  by_action_success: {},
  by_action_failure: {},
  by_throttled: { r: 148 },
  by_snoozed: { f: 63 },
  by_notification: {},
};

it("renders the six KPI tiles from snapshot + totals", () => {
  render(
    <StatTiles
      snapshot={{ by_state: {}, total_hits: 1284, open: 312, ack: 97, closed: 875 }}
      totals={totals}
    />,
  );
  expect(screen.getByText("1284")).toBeInTheDocument();
  expect(screen.getByText("Open")).toBeInTheDocument();
  expect(screen.getByText("148")).toBeInTheDocument();
  expect(screen.getByText("63")).toBeInTheDocument();
});

it("gives each tile a semantic icon and a left accent color", () => {
  const { container } = render(
    <StatTiles
      snapshot={{ by_state: {}, total_hits: 1284, open: 312, ack: 97, closed: 875 }}
      totals={totals}
    />,
  );
  // Icons, in tile order: Total, Open, Ack, Closed, Throttled, Snoozed.
  const hrefs = Array.from(container.querySelectorAll("use")).map((u) => u.getAttribute("href"));
  expect(hrefs).toEqual([
    "/web/icons.svg#icon-layers",
    "/web/icons.svg#icon-bell",
    "/web/icons.svg#icon-check",
    "/web/icons.svg#icon-check-circle",
    "/web/icons.svg#icon-filter",
    "/web/icons.svg#icon-bell-off",
  ]);
  // Each tile carries its accent as a CSS custom property driving the bar + icon.
  expect(container.querySelectorAll('[style*="--tile-accent"]')).toHaveLength(6);
});

it("inverts the ack/closed accents — ack is green, closed is purple", () => {
  render(
    <StatTiles
      snapshot={{ by_state: {}, total_hits: 1284, open: 312, ack: 97, closed: 875 }}
      totals={totals}
    />,
  );
  const ackTile = screen.getByText("Ack").closest("[style]");
  const closedTile = screen.getByText("Closed").closest("[style]");
  expect(ackTile?.getAttribute("style")).toContain("var(--severity-ok)");
  expect(closedTile?.getAttribute("style")).toContain("var(--state-closed)");
});

const snapshot = { by_state: {}, total_hits: 1284, open: 312, ack: 97, closed: 875 };

describe("clickable tiles", () => {
  it("renders plain (non-interactive) tiles when onTileClick is omitted", () => {
    render(<StatTiles snapshot={snapshot} totals={totals} />);
    expect(screen.queryAllByRole("button")).toHaveLength(0);
  });

  it("navigates queryable tiles to their alerts tab", async () => {
    const onTileClick = vi.fn();
    const user = userEvent.setup();
    render(<StatTiles snapshot={snapshot} totals={totals} onTileClick={onTileClick} />);

    // Total → all, Ack → ack, Closed → closed, Snoozed → snoozed.
    await user.click(screen.getByText("Total"));
    expect(onTileClick).toHaveBeenLastCalledWith("all");
    await user.click(screen.getByText("Ack"));
    expect(onTileClick).toHaveBeenLastCalledWith("ack");
    await user.click(screen.getByText("Closed"));
    expect(onTileClick).toHaveBeenLastCalledWith("closed");
    await user.click(screen.getByText("Snoozed"));
    expect(onTileClick).toHaveBeenLastCalledWith("snoozed");
  });

  it("routes the Open tile to the default landing tab", async () => {
    const onTileClick = vi.fn();
    const user = userEvent.setup();
    render(<StatTiles snapshot={snapshot} totals={totals} onTileClick={onTileClick} />);
    await user.click(screen.getByText("Open"));
    expect(onTileClick).toHaveBeenLastCalledWith("alerts");
  });

  it("leaves the Throttled tile non-clickable — no queryable record field", () => {
    const onTileClick = vi.fn();
    render(<StatTiles snapshot={snapshot} totals={totals} onTileClick={onTileClick} />);
    // 5 of 6 tiles are buttons; Throttled stays a plain card.
    expect(screen.getAllByRole("button")).toHaveLength(5);
    const throttledTile = screen.getByText("Throttled").closest("[style]");
    expect(throttledTile?.tagName).toBe("DIV");
  });
});

describe("trend deltas", () => {
  it("renders a ▲ percent badge for range-scoped tiles when a delta is given", () => {
    render(
      <StatTiles snapshot={snapshot} totals={totals} deltas={{ throttled: 12.4, snoozed: -50 }} />,
    );
    expect(screen.getByLabelText("+12% vs prior period")).toBeInTheDocument();
    expect(screen.getByLabelText("-50% vs prior period")).toBeInTheDocument();
    expect(screen.getByText(/▲/)).toBeInTheDocument();
    expect(screen.getByText(/▼/)).toBeInTheDocument();
  });

  it("omits the badge for tiles without a delta (null/absent)", () => {
    render(<StatTiles snapshot={snapshot} totals={totals} deltas={{ throttled: null }} />);
    expect(screen.queryByText(/▲|▼/)).not.toBeInTheDocument();
  });
});
