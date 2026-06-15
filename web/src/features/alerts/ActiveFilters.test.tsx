import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ActiveFilters } from "./ActiveFilters";

function renderStrip(props: Partial<React.ComponentProps<typeof ActiveFilters>> = {}) {
  const handlers = {
    onRemoveEnv: vi.fn(),
    onClearTab: vi.fn(),
    onClearAll: vi.fn(),
  };
  render(
    <ActiveFilters
      tab="ack"
      envs={["env-1"]}
      envName={(uid) => (uid === "env-1" ? "Production" : uid)}
      {...handlers}
      {...props}
    />,
  );
  return handlers;
}

describe("ActiveFilters", () => {
  it("renders a chip per tab/env filter and a Clear all button", () => {
    renderStrip();
    // Tab chip (non-default) shows the tab's label, not its id.
    expect(screen.getByText("Acknowledged")).toBeInTheDocument();
    // Env chip resolves the UID to its display name.
    expect(screen.getByText("Production")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /clear all/i })).toBeInTheDocument();
  });

  it("omits the tab chip for the default 'alerts' tab", () => {
    renderStrip({ tab: "alerts", envs: [] });
    expect(screen.queryByText(/^tab$/i)).toBeNull();
    // Only Clear all remains.
    expect(screen.getByRole("button", { name: /clear all/i })).toBeInTheDocument();
  });

  it("never renders a search chip — the SearchBar owns that display", () => {
    renderStrip();
    // The strip carries tab + env only; no "Search" label is rendered even
    // when a tab/env filter is active.
    expect(screen.queryByText(/^search$/i)).toBeNull();
    expect(screen.queryByRole("button", { name: /remove search filter/i })).toBeNull();
  });

  it("fires the matching remover for each chip's × button", async () => {
    const user = userEvent.setup();
    const h = renderStrip();
    await user.click(screen.getByRole("button", { name: /remove tab filter/i }));
    expect(h.onClearTab).toHaveBeenCalledTimes(1);
    await user.click(screen.getByRole("button", { name: /remove env filter: production/i }));
    expect(h.onRemoveEnv).toHaveBeenCalledWith("env-1");
    await user.click(screen.getByRole("button", { name: /clear all/i }));
    expect(h.onClearAll).toHaveBeenCalledTimes(1);
  });

  it("renders one env chip per selected environment", () => {
    renderStrip({
      envs: ["a", "b"],
      envName: (uid) => ({ a: "Alpha", b: "Bravo" })[uid] ?? uid,
    });
    expect(screen.getByText("Alpha")).toBeInTheDocument();
    expect(screen.getByText("Bravo")).toBeInTheDocument();
  });
});
