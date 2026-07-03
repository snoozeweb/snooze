import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { TimeConstraintsEditor } from "./TimeConstraintsEditor";
import { summarizeTimeConstraints } from "./timeConstraintsUtils";
import type { TimeConstraintsGroup } from "@/lib/timeconstraints/types";

// The zone the editor captures for recurring families in this environment.
const BROWSER_TZ = Intl.DateTimeFormat().resolvedOptions().timeZone;

describe("TimeConstraintsEditor", () => {
  it("toggles a weekday pill in and stamps the browser timezone", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<TimeConstraintsEditor value={{}} onChange={onChange} />);
    await user.click(screen.getByRole("button", { name: "Mon", pressed: false }));
    // Adding a recurring family stamps the zone it is interpreted in.
    expect(onChange).toHaveBeenCalledWith({ weekdays: [{ weekdays: [1] }], tz: BROWSER_TZ });
  });

  it("removes a weekday when toggled off and drops the now-moot tz", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <TimeConstraintsEditor
        value={{ weekdays: [{ weekdays: [1] }], tz: BROWSER_TZ }}
        onChange={onChange}
      />,
    );
    await user.click(screen.getByRole("button", { name: "Mon", pressed: true }));
    // Empty group — the weekdays family AND the tz are dropped entirely.
    expect(onChange).toHaveBeenCalledWith({});
  });

  it("preserves an existing tz (a rule authored in another zone) on edit", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn<(g: TimeConstraintsGroup) => void>();
    render(
      <TimeConstraintsEditor
        value={{ time: [{ from: "09:00", until: "17:00" }], tz: "America/New_York" }}
        onChange={onChange}
      />,
    );
    await user.click(screen.getByRole("button", { name: "Tue", pressed: false }));
    const arg = onChange.mock.calls.at(-1)![0];
    expect(arg.tz).toBe("America/New_York");
    expect(arg.weekdays).toEqual([{ weekdays: [2] }]);
  });

  it("shows the zone the recurring families are interpreted in", () => {
    render(
      <TimeConstraintsEditor
        value={{ weekdays: [{ weekdays: [1] }], tz: "Europe/Paris" }}
        onChange={() => {}}
      />,
    );
    expect(screen.getByText(/interpreted in Europe\/Paris/)).toBeInTheDocument();
  });

  it("does not show a zone note when only absolute date ranges are set", () => {
    render(
      <TimeConstraintsEditor
        value={{ datetime: [{ from: "2026-01-01T08:00:00+01:00" }] }}
        onChange={() => {}}
      />,
    );
    expect(screen.queryByText(/interpreted in/)).not.toBeInTheDocument();
  });

  it("does not claim a zone for a recurring family that carries no persisted tz", () => {
    // A pre-FU-2 / API-authored rule has no tz — the backend reads it as UTC,
    // so the note must not assert the browser's zone until an edit stamps it.
    render(<TimeConstraintsEditor value={{ weekdays: [{ weekdays: [1] }] }} onChange={() => {}} />);
    expect(screen.queryByText(/interpreted in/)).not.toBeInTheDocument();
  });
});

describe("summarizeTimeConstraints", () => {
  it("returns 'always' for an empty group", () => {
    expect(summarizeTimeConstraints({})).toBe("always");
    expect(summarizeTimeConstraints(undefined)).toBe("—");
  });

  it("summarises weekdays + time windows", () => {
    expect(
      summarizeTimeConstraints({
        weekdays: [{ weekdays: [1, 2, 3, 4, 5] }],
        time: [{ from: "09:00", until: "18:00" }],
      }),
    ).toBe("Mon,Tue,Wed,Thu,Fri · 09:00-18:00");
  });

  it("calls out the every-day case explicitly", () => {
    expect(summarizeTimeConstraints({ weekdays: [{ weekdays: [0, 1, 2, 3, 4, 5, 6] }] })).toBe(
      "every day",
    );
  });
});
