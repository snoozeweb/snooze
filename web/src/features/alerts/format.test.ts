import { describe, expect, it, vi } from "vitest";
import {
  formatCountdown,
  formatRelativeTime,
  formatShelveUntil,
  formatTTL,
  humanDuration,
  severityBadgeVariant,
  severityDisplayLabel,
  stateBadgeVariant,
  stateLabel,
  trendLabel,
  trimDate,
} from "./format";

describe("severityDisplayLabel", () => {
  it.each([
    ["err", "Error"],
    ["ERR", "Error"],
    ["crit", "Critical"],
    ["warn", "Warning"],
    ["ok", "OK"],
    ["info", "Info"],
  ])("title-cases the raw token %s -> %s", (raw, expected) => {
    expect(severityDisplayLabel(raw)).toBe(expected);
  });

  it("falls back to capitalizing an unknown token instead of hiding it", () => {
    expect(severityDisplayLabel("weird")).toBe("Weird");
  });
});

describe("severityBadgeVariant", () => {
  it.each([
    // syslog top of the ladder collapses to the red `critical` badge so
    // emergencies don't slip into muted/black like they did before.
    ["emerg", "critical"],
    ["emergency", "critical"],
    ["EMERGENCY", "critical"],
    ["panic", "critical"],
    ["alert", "critical"],
    ["fatal", "critical"],
    ["crit", "critical"],
    ["critical", "critical"],
    ["Critical", "critical"],
    ["err", "error"],
    ["error", "error"],
    ["ERROR", "error"],
    ["fail", "error"],
    ["failure", "error"],
    ["warn", "warning"],
    ["warning", "warning"],
    ["Warning", "warning"],
    ["notice", "info"],
    ["info", "info"],
    ["informational", "info"],
    ["ok", "ok"],
    ["okay", "ok"],
    ["success", "ok"],
    // Surrounding whitespace shouldn't kick a known severity into muted.
    ["  warning  ", "warning"],
  ] as const)("maps %s to %s", (sev, expected) => {
    expect(severityBadgeVariant(sev)).toBe(expected);
  });

  it("falls back to muted for unknown / empty severities", () => {
    expect(severityBadgeVariant("debug")).toBe("muted");
    expect(severityBadgeVariant("trace")).toBe("muted");
    expect(severityBadgeVariant("")).toBe("muted");
  });
});

describe("stateLabel + stateBadgeVariant", () => {
  it("formats open as 'Open' with neutral variant (lifecycle, not urgency)", () => {
    expect(stateLabel("open")).toBe("Open");
    expect(stateBadgeVariant("open")).toBe("neutral");
  });

  it("ack/esc/close/shelved map to appropriate variants", () => {
    // ack → the violet "ack" variant, matching the timeline/feed hue (a
    // human owns it) instead of the OK-green severity token.
    expect(stateBadgeVariant("ack")).toBe("ack");
    expect(stateBadgeVariant("esc")).toBe("warning");
    // close → the muted-purple "closed" variant; shelved stays muted gray.
    expect(stateBadgeVariant("close")).toBe("closed");
    expect(stateBadgeVariant("shelved")).toBe("muted");
  });

  it("uses the canonical lifecycle nouns — same word as the tab/tile/legend", () => {
    expect(stateLabel("ack")).toBe("Acknowledged");
    expect(stateLabel("esc")).toBe("Re-escalated");
    expect(stateLabel("close")).toBe("Closed");
    expect(stateLabel("shelved")).toBe("Shelved");
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

describe("formatCountdown", () => {
  it("returns '' for zero (no deadline)", () => {
    expect(formatCountdown(0)).toBe("");
  });

  it("returns '' for undefined", () => {
    expect(formatCountdown(undefined)).toBe("");
  });

  it("returns '' when deadline is in the past", () => {
    const pastEpoch = Math.floor(Date.now() / 1000) - 3600;
    expect(formatCountdown(pastEpoch)).toBe("");
  });

  it("returns 'in Xh' for a future deadline 3 hours away", () => {
    vi.useFakeTimers();
    const now = new Date("2026-06-30T12:00:00Z");
    vi.setSystemTime(now);
    try {
      const futureEpoch = Math.floor(now.getTime() / 1000) + 3 * 3600;
      expect(formatCountdown(futureEpoch)).toBe("in 3h");
    } finally {
      vi.useRealTimers();
    }
  });

  it("returns 'in Xh Ym' for a deadline with hours and minutes remaining", () => {
    vi.useFakeTimers();
    const now = new Date("2026-06-30T12:00:00Z");
    vi.setSystemTime(now);
    try {
      const futureEpoch = Math.floor(now.getTime() / 1000) + 2 * 3600 + 30 * 60;
      expect(formatCountdown(futureEpoch)).toBe("in 2h 30m");
    } finally {
      vi.useRealTimers();
    }
  });
});

describe("trendLabel", () => {
  it("returns 'Severity escalated' for moreSevere", () => {
    expect(trendLabel("moreSevere")).toBe("Severity escalated");
  });

  it("returns 'Severity decreased' for lessSevere", () => {
    expect(trendLabel("lessSevere")).toBe("Severity decreased");
  });

  it("returns 'No change' for noChange", () => {
    expect(trendLabel("noChange")).toBe("No change");
  });

  it("returns '' for empty string", () => {
    expect(trendLabel("")).toBe("");
  });

  it("returns '' for unknown trend value", () => {
    expect(trendLabel("unknown")).toBe("");
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

describe("humanDuration (exported)", () => {
  it("is exported and returns a non-empty string for positive input", () => {
    expect(humanDuration(3600)).toBe("1h");
    expect(humanDuration(3661)).toBe("1h 1m");
    expect(humanDuration(86400)).toBe("1d");
    expect(humanDuration(90061)).toBe("1d 1h");
    expect(humanDuration(45)).toBe("45s");
  });
});

describe("formatShelveUntil", () => {
  it("returns '' for undefined", () => {
    expect(formatShelveUntil(undefined)).toBe("");
  });

  it("returns '' for zero", () => {
    expect(formatShelveUntil(0)).toBe("");
  });

  it("returns 'expired (pending sweep)' when deadline is in the past", () => {
    const past = Math.floor(Date.now() / 1000) - 3600;
    expect(formatShelveUntil(past)).toBe("expired (pending sweep)");
  });

  it("returns 'returns in Xh' for a future deadline 2 hours away", () => {
    vi.useFakeTimers();
    const now = new Date("2026-06-30T12:00:00Z");
    vi.setSystemTime(now);
    try {
      const futureEpoch = Math.floor(now.getTime() / 1000) + 2 * 3600;
      expect(formatShelveUntil(futureEpoch)).toBe("returns in 2h");
    } finally {
      vi.useRealTimers();
    }
  });

  it("returns 'returns in Xh Ym' for a deadline with hours and minutes", () => {
    vi.useFakeTimers();
    const now = new Date("2026-06-30T12:00:00Z");
    vi.setSystemTime(now);
    try {
      const futureEpoch = Math.floor(now.getTime() / 1000) + 3 * 3600 + 30 * 60;
      expect(formatShelveUntil(futureEpoch)).toBe("returns in 3h 30m");
    } finally {
      vi.useRealTimers();
    }
  });
});
