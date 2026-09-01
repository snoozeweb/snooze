import { render, screen, within } from "@testing-library/react";
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

const snapshot = { by_state: {}, total_hits: 1284, open: 312, ack: 97, closed: 875 };

function renderTiles(props: Partial<React.ComponentProps<typeof StatTiles>> = {}) {
  return render(
    <StatTiles
      snapshot={snapshot}
      totals={totals}
      needsAttention={214}
      windowLabel="Last 24 hours"
      {...props}
    />,
  );
}

it("renders the live and windowed clusters from snapshot + totals", () => {
  renderTiles();
  const live = screen.getByRole("region", { name: "Right now" });
  const windowed = screen.getByRole("region", { name: "Last 24 hours" });

  // Live cluster: the counts the alerts table also shows.
  expect(within(live).getByText("214")).toBeInTheDocument();
  expect(within(live).getByText("Needs attention")).toBeInTheDocument();
  expect(within(live).getByText("Acknowledged")).toBeInTheDocument();

  // Windowed cluster: counter-backed sums, suppression first.
  expect(within(windowed).getByText("148")).toBeInTheDocument();
  expect(within(windowed).getByText("63")).toBeInTheDocument();
  // Group separator is locale-dependent (thin space, comma, dot…): match on
  // the digits with any single separator between them.
  expect(within(windowed).getByText(/^1\D?284$/)).toBeInTheDocument();
});

it("keeps the windowed counts out of the live cluster — they answer different questions", () => {
  renderTiles();
  const live = screen.getByRole("region", { name: "Right now" });
  expect(within(live).queryByText("Ingested")).not.toBeInTheDocument();
  expect(within(live).queryByText("Throttled")).not.toBeInTheDocument();
});

it("gives each tile a semantic icon and an accent color", () => {
  const { container } = renderTiles();
   // Icons, in tile order: Needs attention, Acknowledged | Throttled, Snoozed, Ingested.
  const hrefs = Array.from(container.querySelectorAll("use")).map((u) => u.getAttribute("href"));
  expect(hrefs).toEqual([
    "/web/icons.svg#icon-bell",
    "/web/icons.svg#icon-check",
    "/web/icons.svg#icon-filter",
    "/web/icons.svg#icon-bell-off",
    "/web/icons.svg#icon-layers",
  ]);
  expect(container.querySelectorAll('[style*="--tile-accent"]')).toHaveLength(5);
});

describe("clickable tiles", () => {
  it("renders plain (non-interactive) tiles when onTileClick is omitted", () => {
    renderTiles();
    expect(screen.queryAllByRole("button")).toHaveLength(0);
  });

  it("navigates the live tiles to their alerts tab", async () => {
    const onTileClick = vi.fn();
    const user = userEvent.setup();
    renderTiles({ onTileClick });

    await user.click(screen.getByText("Needs attention"));
    expect(onTileClick).toHaveBeenLastCalledWith("alerts");
    await user.click(screen.getByText("Acknowledged"));
    expect(onTileClick).toHaveBeenLastCalledWith("ack");
  });

  it("leaves the windowed tiles non-clickable — event counts are not a list of alerts", () => {
    const onTileClick = vi.fn();
    renderTiles({ onTileClick });
    // Only the two live tiles are buttons.
    expect(screen.getAllByRole("button")).toHaveLength(2);
    expect(screen.getByText("Throttled").closest("[style]")?.tagName).toBe("DIV");
    expect(screen.getByText("Ingested").closest("[style]")?.tagName).toBe("DIV");
  });
});

describe("trend deltas", () => {
  it("renders a ▲ percent badge for windowed tiles when a delta is given", () => {
    renderTiles({ deltas: { throttled: 12.4, snoozed: -50 } });
    expect(screen.getByLabelText("+12% vs prior period")).toBeInTheDocument();
    expect(screen.getByLabelText("-50% vs prior period")).toBeInTheDocument();
    expect(screen.getByText(/▲/)).toBeInTheDocument();
    expect(screen.getByText(/▼/)).toBeInTheDocument();
  });

  it("omits the badge for tiles without a delta (null/absent)", () => {
    renderTiles({ deltas: { throttled: null } });
    expect(screen.queryByText(/▲|▼/)).not.toBeInTheDocument();
  });
});
