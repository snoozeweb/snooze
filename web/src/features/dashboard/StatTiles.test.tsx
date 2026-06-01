import { render, screen } from "@testing-library/react";
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
