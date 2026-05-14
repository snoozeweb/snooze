import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { AlertsFilters, type AlertFilters } from "./Filters";

function harness(initial: AlertFilters = {}) {
  const onChange = vi.fn();
  render(<AlertsFilters value={initial} onChange={onChange} />);
  return { onChange };
}

describe("AlertsFilters", () => {
  it("renders state + severity selects and environment + search inputs", () => {
    harness();
    expect(screen.getAllByRole("combobox").length).toBeGreaterThanOrEqual(2);
    expect(screen.getByPlaceholderText(/environment/i)).toBeInTheDocument();
    expect(screen.getByPlaceholderText(/search/i)).toBeInTheDocument();
  });

  it("emits onChange when severity dropdown changes", async () => {
    const user = userEvent.setup();
    const { onChange } = harness();
    // Click the severity combobox (placeholder includes "Severity")
    const triggers = screen.getAllByRole("combobox");
    const severityTrigger = triggers.find((el) =>
      (el.textContent ?? "").toLowerCase().includes("severity"),
    );
    expect(severityTrigger).toBeTruthy();
    await user.click(severityTrigger!);
    await user.click(screen.getByRole("option", { name: /critical/i }));
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ severity: "critical" }));
  });

  it("debounced search emits the final value", async () => {
    const user = userEvent.setup();
    const { onChange } = harness();
    await user.type(screen.getByPlaceholderText(/search/i), "srv-1");
    // wait for the 300ms debounce
    await new Promise((r) => setTimeout(r, 400));
    expect(onChange).toHaveBeenCalled();
    const last = onChange.mock.calls.at(-1)?.[0] as AlertFilters;
    expect(last.search).toBe("srv-1");
  });

  it("(all) option clears the filter", async () => {
    const user = userEvent.setup();
    const { onChange } = harness({ severity: "critical" });
    const triggers = screen.getAllByRole("combobox");
    const severityTrigger = triggers.find((el) =>
      (el.textContent ?? "").toLowerCase().includes("critical"),
    );
    expect(severityTrigger).toBeTruthy();
    await user.click(severityTrigger!);
    await user.click(screen.getByRole("option", { name: /all severities/i }));
    const last = onChange.mock.calls.at(-1)?.[0] as AlertFilters;
    expect(last.severity).toBeUndefined();
  });
});
