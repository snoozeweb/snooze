import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { TimeRangePicker, type TimeRange } from "./TimeRangePicker";

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

  it("shows the shared datetime range picker trigger when range=custom", () => {
    const value: TimeRange = { range: "custom", from: "", to: "" };
    render(<TimeRangePicker value={value} onChange={() => undefined} />);
    // The shared DateTimeRangePicker renders a single trigger button whose
    // accessible name embeds both From/Until aria labels.
    expect(screen.getByRole("button", { name: /From \/ Until/ })).toBeInTheDocument();
  });
});
