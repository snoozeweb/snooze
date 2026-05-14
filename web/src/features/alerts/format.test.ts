import { describe, expect, it } from "vitest";
import {
  formatRelativeTime,
  severityBadgeVariant,
  stateBadgeVariant,
  stateLabel,
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
  it("formats open as 'Open' with neutral variant", () => {
    expect(stateLabel("open")).toBe("Open");
    expect(stateBadgeVariant("open")).toBe("neutral");
  });

  it("ack/close/shelved map to appropriate variants", () => {
    expect(stateBadgeVariant("ack")).toBe("info");
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
