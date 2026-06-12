import { describe, expect, it, vi } from "vitest";
import { formatRelativeTime, trimDate } from "./time";

describe("formatRelativeTime", () => {
  it("returns 'just now' for < 60s", () => {
    const dateEpoch = Math.floor(Date.now() / 1000) - 5;
    expect(formatRelativeTime(dateEpoch)).toMatch(/just now|5s/);
  });

  it("returns minutes for < 1h", () => {
    const dateEpoch = Math.floor(Date.now() / 1000) - 5 * 60;
    expect(formatRelativeTime(dateEpoch)).toMatch(/5m/);
  });

  it("returns hours for < 1d", () => {
    const dateEpoch = Math.floor(Date.now() / 1000) - 3 * 3600;
    expect(formatRelativeTime(dateEpoch)).toMatch(/3h/);
  });

  it("returns days for >= 1d", () => {
    const dateEpoch = Math.floor(Date.now() / 1000) - 2 * 86400;
    expect(formatRelativeTime(dateEpoch)).toMatch(/2d/);
  });

  it("handles undefined / 0 gracefully", () => {
    expect(formatRelativeTime(undefined)).toBe("—");
    expect(formatRelativeTime(0)).toBe("—");
  });
});

describe("trimDate", () => {
  function withClock(t: Date, fn: () => void) {
    vi.useFakeTimers();
    vi.setSystemTime(t);
    try {
      fn();
    } finally {
      vi.useRealTimers();
    }
  }

  it("returns 'Today HH:mm' for same-day timestamps", () => {
    const now = new Date(2026, 4, 22, 14, 30); // 2026-05-22 14:30
    withClock(now, () => {
      const ts = Math.floor(new Date(2026, 4, 22, 9, 5).getTime() / 1000);
      expect(trimDate(ts)).toBe("Today 09:05");
    });
  });

  it("returns 'MMM Do HH:mm' for same-year different-day timestamps", () => {
    const now = new Date(2026, 4, 22, 14, 30);
    withClock(now, () => {
      const ts = Math.floor(new Date(2026, 10, 3, 9, 5).getTime() / 1000);
      expect(trimDate(ts)).toBe("Nov 3rd 09:05");
    });
  });

  it("uses the correct ordinal suffix for 11th/21st/22nd", () => {
    const now = new Date(2026, 4, 22, 14, 30);
    withClock(now, () => {
      expect(trimDate(Math.floor(new Date(2026, 0, 11, 8, 0).getTime() / 1000))).toBe(
        "Jan 11th 08:00",
      );
      expect(trimDate(Math.floor(new Date(2026, 0, 21, 8, 0).getTime() / 1000))).toBe(
        "Jan 21st 08:00",
      );
      expect(trimDate(Math.floor(new Date(2026, 0, 22, 8, 0).getTime() / 1000))).toBe(
        "Jan 22nd 08:00",
      );
    });
  });

  it("returns 'MMM Do YYYY' for different-year timestamps", () => {
    const now = new Date(2026, 4, 22, 14, 30);
    withClock(now, () => {
      const ts = Math.floor(new Date(2024, 0, 1, 8, 0).getTime() / 1000);
      expect(trimDate(ts)).toBe("Jan 1st 2024");
    });
  });

  it("returns '—' for undefined / 0", () => {
    expect(trimDate(undefined)).toBe("—");
    expect(trimDate(0)).toBe("—");
  });
});
