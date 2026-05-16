import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { TimeConstraintsEditor } from "./TimeConstraintsEditor";
import { summarizeTimeConstraints } from "./timeConstraintsUtils";

describe("TimeConstraintsEditor", () => {
  it("toggles a weekday pill into the constraint group", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<TimeConstraintsEditor value={{}} onChange={onChange} />);
    await user.click(screen.getByRole("button", { name: "Mon", pressed: false }));
    expect(onChange).toHaveBeenCalledWith({ weekdays: [{ weekdays: [1] }] });
  });

  it("removes a weekday when toggled off", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <TimeConstraintsEditor
        value={{ weekdays: [{ weekdays: [1] }] }}
        onChange={onChange}
      />,
    );
    await user.click(screen.getByRole("button", { name: "Mon", pressed: true }));
    // Empty group — the weekdays family is dropped entirely.
    expect(onChange).toHaveBeenCalledWith({});
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
    expect(
      summarizeTimeConstraints({ weekdays: [{ weekdays: [0, 1, 2, 3, 4, 5, 6] }] }),
    ).toBe("every day");
  });
});
