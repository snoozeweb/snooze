// DateTimeRangePicker — TDD tests written before the component.
//
// The component wraps react-day-picker (for date selection) and a pair of
// <input type="time"> spinners inside a Radix Popover. The wire shape
// stays identical to what the old native <input type="datetime-local">
// emitted ("YYYY-MM-DDTHH:MM") and <input type="time"> ("HH:MM"), so the
// Go backend continues to see the same payloads.
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { DateTimeRangePicker } from "./DateTimeRangePicker";

describe("DateTimeRangePicker", () => {
  describe("trigger label", () => {
    it("renders the formatted time range when mode=time and value is set", () => {
      render(
        <DateTimeRangePicker
          mode="time"
          value={{ from: "09:00", until: "17:00" }}
          onChange={() => {}}
          ariaLabelFrom="Time window 1 from"
          ariaLabelUntil="Time window 1 until"
        />,
      );
      expect(screen.getByRole("button")).toHaveTextContent("09:00");
      expect(screen.getByRole("button")).toHaveTextContent("17:00");
    });

    it("renders the formatted datetime range when mode=datetime and value is set", () => {
      render(
        <DateTimeRangePicker
          mode="datetime"
          value={{ from: "2026-05-15T14:00", until: "2026-05-16T08:00" }}
          onChange={() => {}}
          ariaLabelFrom="Date range 1 from"
          ariaLabelUntil="Date range 1 until"
        />,
      );
      const btn = screen.getByRole("button");
      // The chip shows both dates and times — exact formatting is
      // controlled by the component, so we assert the substrings rather
      // than a single string template.
      expect(btn.textContent).toMatch(/2026-05-15/);
      expect(btn.textContent).toMatch(/14:00/);
      expect(btn.textContent).toMatch(/2026-05-16/);
      expect(btn.textContent).toMatch(/08:00/);
    });

    it("renders placeholder text when value is empty (time mode)", () => {
      render(
        <DateTimeRangePicker
          mode="time"
          value={{}}
          onChange={() => {}}
          ariaLabelFrom="Time window 1 from"
          ariaLabelUntil="Time window 1 until"
        />,
      );
      expect(screen.getByRole("button")).toHaveTextContent(/--:--/);
    });

    it("renders placeholder text when value is empty (datetime mode)", () => {
      render(
        <DateTimeRangePicker
          mode="datetime"
          value={{}}
          onChange={() => {}}
          ariaLabelFrom="Date range 1 from"
          ariaLabelUntil="Date range 1 until"
        />,
      );
      // Datetime placeholder is more verbose than the time variant — we
      // just check that *some* placeholder dash glyph is present.
      expect(screen.getByRole("button").textContent ?? "").toMatch(/-/);
    });
  });

  describe("mode=time", () => {
    it("opens a popover with two time inputs on click", async () => {
      const user = userEvent.setup();
      render(
        <DateTimeRangePicker
          mode="time"
          value={{ from: "09:00", until: "17:00" }}
          onChange={() => {}}
          ariaLabelFrom="Time window 1 from"
          ariaLabelUntil="Time window 1 until"
        />,
      );
      // Popover content is portaled — should not exist before click.
      expect(screen.queryByLabelText("Time window 1 from")).toBeNull();
      await user.click(screen.getByRole("button"));
      // After opening, the two time inputs are accessible via their
      // ariaLabel{From,Until} props.
      expect(screen.getByLabelText("Time window 1 from")).toBeInTheDocument();
      expect(screen.getByLabelText("Time window 1 until")).toBeInTheDocument();
    });

    it("emits HH:MM strings unchanged when the from-time changes", async () => {
      const user = userEvent.setup();
      const onChange = vi.fn<(next: { from?: string; until?: string }) => void>();
      render(
        <DateTimeRangePicker
          mode="time"
          value={{ from: "09:00", until: "17:00" }}
          onChange={onChange}
          ariaLabelFrom="Time window 1 from"
          ariaLabelUntil="Time window 1 until"
        />,
      );
      await user.click(screen.getByRole("button"));
      const fromInput = screen.getByLabelText("Time window 1 from");
      // jsdom's <input type="time"> accepts fireEvent.change reliably;
      // userEvent.type on these is finicky, so we use .clear+.type with
      // a delay-free pace.
      await user.clear(fromInput);
      await user.type(fromInput, "10:30");
      // The handler should have been called at least once with the new
      // from value (intermediate keystrokes may also fire — we just
      // check the last call's payload).
      expect(onChange).toHaveBeenCalled();
      const last = onChange.mock.calls.at(-1)?.[0];
      expect(last).toMatchObject({ until: "17:00" });
      // The emitted "from" must be a HH:MM string (matching the legacy
      // wire shape). jsdom may report partial typed states, but the
      // string format must hold.
      expect(typeof last?.from).toBe("string");
      expect(last?.from).toMatch(/^\d{2}:\d{2}$/);
    });
  });

  describe("mode=datetime", () => {
    it("opens a popover with a calendar AND two time inputs on click", async () => {
      const user = userEvent.setup();
      render(
        <DateTimeRangePicker
          mode="datetime"
          value={{ from: "2026-05-15T14:00", until: "2026-05-16T08:00" }}
          onChange={() => {}}
          ariaLabelFrom="Date range 1 from"
          ariaLabelUntil="Date range 1 until"
        />,
      );
      await user.click(screen.getByRole("button"));
      // The two time inputs use the from/until aria labels.
      expect(screen.getByLabelText("Date range 1 from")).toBeInTheDocument();
      expect(screen.getByLabelText("Date range 1 until")).toBeInTheDocument();
      // The react-day-picker root has role="grid" inside (the days
      // table). v9 renders the calendar as a <table role="grid">.
      expect(screen.getByRole("grid")).toBeInTheDocument();
    });

    it("emits ISO-local strings (no seconds, no Z) when the from-time changes", async () => {
      const user = userEvent.setup();
      const onChange = vi.fn<(next: { from?: string; until?: string }) => void>();
      render(
        <DateTimeRangePicker
          mode="datetime"
          value={{ from: "2026-05-15T14:00", until: "2026-05-16T08:00" }}
          onChange={onChange}
          ariaLabelFrom="Date range 1 from"
          ariaLabelUntil="Date range 1 until"
        />,
      );
      await user.click(screen.getByRole("button"));
      const fromInput = screen.getByLabelText("Date range 1 from");
      await user.clear(fromInput);
      await user.type(fromInput, "10:30");
      expect(onChange).toHaveBeenCalled();
      const last = onChange.mock.calls.at(-1)?.[0];
      // Wire shape: "YYYY-MM-DDTHH:MM" — exactly what
      // <input type="datetime-local"> produces today. No seconds, no
      // timezone suffix.
      expect(last?.from).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/);
      expect(last?.until).toBe("2026-05-16T08:00");
    });
  });

  describe("accessibility", () => {
    it("propagates aria labels to the inner time inputs", async () => {
      const user = userEvent.setup();
      render(
        <DateTimeRangePicker
          mode="time"
          value={{ from: "09:00", until: "17:00" }}
          onChange={() => {}}
          ariaLabelFrom="Custom From Label"
          ariaLabelUntil="Custom Until Label"
        />,
      );
      await user.click(screen.getByRole("button"));
      expect(screen.getByLabelText("Custom From Label")).toBeInTheDocument();
      expect(screen.getByLabelText("Custom Until Label")).toBeInTheDocument();
    });
  });
});
