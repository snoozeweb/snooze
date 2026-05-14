import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { TimeRangePicker, presetToRange, type TimeRange } from "./TimeRangePicker";

describe("presetToRange", () => {
  it("returns a 24h window for '1d'", () => {
    const now = new Date("2026-05-14T12:00:00Z");
    const r = presetToRange("1d", now);
    expect(r.to).toBe("2026-05-14T12:00:00.000Z");
    expect(r.from).toBe("2026-05-13T12:00:00.000Z");
  });
  it("returns empty strings for 'custom'", () => {
    expect(presetToRange("custom").from).toBe("");
  });
});

describe("TimeRangePicker", () => {
  it("highlights the active preset", () => {
    const value: TimeRange = { range: "1w", from: "x", to: "y" };
    render(<TimeRangePicker value={value} onChange={() => undefined} />);
    expect(screen.getByRole("button", { name: "1w" })).toHaveAttribute("data-active", "true");
  });

  it("emits onChange with derived from/to when clicking a preset", async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    const value: TimeRange = { range: "1d", from: "x", to: "y" };
    render(<TimeRangePicker value={value} onChange={onChange} />);
    await user.click(screen.getByRole("button", { name: "1m" }));
    expect(onChange).toHaveBeenCalled();
    const arg = onChange.mock.calls[0]?.[0] as TimeRange;
    expect(arg.range).toBe("1m");
    expect(arg.from).toMatch(/^\d{4}-\d{2}-\d{2}T/);
  });

  it("shows date inputs when range=custom", () => {
    const value: TimeRange = { range: "custom", from: "", to: "" };
    render(<TimeRangePicker value={value} onChange={() => undefined} />);
    // Two date Inputs, each renders a textbox-with-type=date.
    expect(screen.getAllByDisplayValue("").length).toBeGreaterThanOrEqual(2);
  });
});
