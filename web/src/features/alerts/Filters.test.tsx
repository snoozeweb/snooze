import { useState } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AlertsFilters, type AlertFilters } from "./Filters";

// The harness stays controlled: parent owns the value, threads onChange
// updates back into it via useState. The Filters component is fully
// controlled — without parent-side state propagation, the SearchBar input
// is reset on every render and only the last typed character survives.
function harness(initial: AlertFilters = {}) {
  const onChange = vi.fn();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  function Wrapper() {
    const [value, setValue] = useState<AlertFilters>(initial);
    return (
      <QueryClientProvider client={client}>
        <AlertsFilters
          value={value}
          onChange={(next) => {
            setValue(next);
            onChange(next);
          }}
        />
      </QueryClientProvider>
    );
  }
  render(<Wrapper />);
  return { onChange };
}

describe("AlertsFilters", () => {
  it("renders the seven lifecycle tabs and the search bar", () => {
    harness();
    for (const label of [
      "Alerts",
      "Snoozed",
      "Acknowledged",
      "Re-escalated",
      "Closed",
      "Shelved",
      "All",
    ]) {
      expect(screen.getByRole("tab", { name: label })).toBeInTheDocument();
    }
    expect(screen.getByRole("textbox", { name: "Search" })).toBeInTheDocument();
  });

  it("marks Alerts as the default active tab when none is selected", () => {
    harness();
    const alertsTab = screen.getByRole("tab", { name: "Alerts" });
    expect(alertsTab).toHaveAttribute("aria-selected", "true");
    const snoozedTab = screen.getByRole("tab", { name: "Snoozed" });
    expect(snoozedTab).toHaveAttribute("aria-selected", "false");
  });

  it("respects an explicit value.tab", () => {
    harness({ tab: "closed" });
    expect(screen.getByRole("tab", { name: "Closed" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
  });

  it("emits onChange with the next tab id when a tab is clicked", async () => {
    const user = userEvent.setup();
    const { onChange } = harness();
    await user.click(screen.getByRole("tab", { name: "Snoozed" }));
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ tab: "snoozed" }));
  });

  it("does not emit when the active tab is re-clicked", async () => {
    const user = userEvent.setup();
    const { onChange } = harness({ tab: "alerts" });
    await user.click(screen.getByRole("tab", { name: "Alerts" }));
    expect(onChange).not.toHaveBeenCalled();
  });

  it("SearchBar text propagates to onChange", async () => {
    const user = userEvent.setup();
    const { onChange } = harness();
    await user.type(screen.getByRole("textbox", { name: "Search" }), "srv-1");
    expect(onChange).toHaveBeenCalled();
    const last = onChange.mock.calls.at(-1)?.[0] as AlertFilters;
    expect(last.search).toBe("srv-1");
  });
});
