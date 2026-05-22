import { describe, expect, it, vi } from "vitest";
import {
  formatRelativeTime,
  formatTTL,
  severityBadgeVariant,
  stateBadgeVariant,
  stateLabel,
  trimDate,
} from "./format";

describe("severityBadgeVariant", () => {
  it.each([
    ["critical", "critical"],
    ["error", "error"],
    ["warning", "warning"],
    ["info", "info"],
  ] as const)("maps %s to %s", (sev, expected) => {
    expect(severityBadgeVariant(sev)).toBe(expected);
  });

  it("falls back to muted for unknown severities", () => {
    expect(severityBadgeVariant("debug")).toBe("muted");
    expect(severityBadgeVariant("")).toBe("muted");
  });
});

describe("stateLabel + stateBadgeVariant", () => {
  it("formats open as 'Open' with neutral variant (lifecycle, not urgency)", () => {
    expect(stateLabel("open")).toBe("Open");
    expect(stateBadgeVariant("open")).toBe("neutral");
  });

  it("ack/esc/close/shelved map to appropriate variants", () => {
    expect(stateBadgeVariant("ack")).toBe("info");
    expect(stateBadgeVariant("esc")).toBe("warning");
    expect(stateBadgeVariant("close")).toBe("muted");
    expect(stateBadgeVariant("shelved")).toBe("muted");
  });
});

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

  it("handles undefined gracefully", () => {
    expect(formatRelativeTime(undefined)).toBe("—");
  });
});

describe("trimDate", () => {
  // Pin clock to a deterministic instant so the same-day / same-year
  // branches are stable on any machine. Date is 2026-05-22 14:30 local
  // (matches the test workspace's clock).
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

describe("formatTTL", () => {
  it("returns 'shelved' for negative ttl regardless of date_epoch", () => {
    expect(formatTTL(-1, 1_700_000_000)).toBe("shelved");
    expect(formatTTL(-172800, undefined)).toBe("shelved");
  });

  it("returns '—' for undefined ttl", () => {
    expect(formatTTL(undefined, 1_700_000_000)).toBe("—");
  });

  it("returns 'expired' when date_epoch + ttl is in the past", () => {
    const longAgo = Math.floor(Date.now() / 1000) - 10_000;
    expect(formatTTL(60, longAgo)).toBe("expired");
  });

  it("returns 'in <duration>' for future expiry", () => {
    const now = Math.floor(Date.now() / 1000);
    // 90 minutes from now = 1h 30m
    const out = formatTTL(90 * 60, now);
    expect(out).toMatch(/^in 1h /);
  });

  it("emits seconds at the minute boundary", () => {
    const now = Math.floor(Date.now() / 1000);
    // 75 seconds from now: 1m + ~15s
    const out = formatTTL(75, now);
    expect(out).toMatch(/^in 1m \d{2}s$|^in 1m$/);
  });
});
