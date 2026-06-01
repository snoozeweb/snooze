import { render, screen } from "@testing-library/react";
import { StatTiles } from "./StatTiles";

it("renders the six KPI tiles from snapshot + totals", () => {
  render(<StatTiles
    snapshot={{ by_state: {}, total_hits: 1284, open: 312, ack: 97, closed: 875 }}
    totals={{ by_throttled: { r: 148 }, by_snoozed: { f: 63 } } as any}
  />);
  expect(screen.getByText("1284")).toBeInTheDocument();
  expect(screen.getByText("Open")).toBeInTheDocument();
  expect(screen.getByText("148")).toBeInTheDocument();
  expect(screen.getByText("63")).toBeInTheDocument();
});
