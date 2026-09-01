import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { Chart } from "chart.js";
import { applyChartDefaults, chartPalette, chartToken, seriesColor } from "./theme";

const TOKENS: Record<string, string> = {
  "--severity-critical": "#ff5952",
  "--severity-error": "#f0883e",
  "--severity-warning": "#e3b341",
  "--severity-info": "#58a6ff",
  "--severity-ok": "#3fb950",
  "--state-ack": "#b083f9",
  "--state-closed": "#84ab90",
  "--state-shelve": "#84a8cf",
  "--state-snooze": "#768390",
  "--text-muted": "#8b96a3",
  "--font-mono": '"IBM Plex Mono", monospace',
};

function setTokens() {
  const root = document.documentElement;
  for (const [k, v] of Object.entries(TOKENS)) root.style.setProperty(k, v);
}

function clearTokens() {
  const root = document.documentElement;
  for (const k of Object.keys(TOKENS)) root.style.removeProperty(k);
}

describe("chartToken", () => {
  afterEach(clearTokens);

  it("resolves a set custom property to its value", () => {
    setTokens();
    expect(chartToken("--severity-info")).toBe("#58a6ff");
  });

  // jsdom's getComputedStyle returns "" for an unset custom property — the
  // same contract the severity-color tests lean on. We must fall back.
  it("falls back to the neutral when the token is unset", () => {
    expect(chartToken("--does-not-exist")).toBe("#6b7785");
  });

  it("honours an explicit fallback argument", () => {
    expect(chartToken("--does-not-exist", "#000")).toBe("#000");
  });
});

describe("chartPalette", () => {
  afterEach(clearTokens);

  it("returns the ordered categorical palette from tokens", () => {
    setTokens();
    expect(chartPalette()).toEqual([
      "#58a6ff", // info
      "#3fb950", // ok
      "#e3b341", // warning
      "#f0883e", // error
      "#ff5952", // critical
      "#b083f9", // ack
      "#84a8cf", // shelve
      "#768390", // snooze
    ]);
  });

  it("falls back to the neutral for every slot when no tokens are set", () => {
    expect(chartPalette().every((c) => c === "#6b7785")).toBe(true);
  });
});

describe("seriesColor", () => {
  beforeEach(setTokens);
  afterEach(clearTokens);

  it("maps known dashboard series to their semantic token", () => {
    expect(seriesColor("Alerts")).toBe("#58a6ff");
    expect(seriesColor("Action error")).toBe("#ff5952");
    expect(seriesColor("Notification sent")).toBe("#3fb950");
    expect(seriesColor("Successful")).toBe("#3fb950");
    expect(seriesColor("Failed")).toBe("#ff5952");
  });

  // The dashboard's "By state" bar feeds raw snapshot.by_state keys through
  // here. Without entries they fell through to the categorical palette by
  // index, so a segment's colour depended on map insertion order rather than
  // on what the state means — and ack/closed came out arbitrary.
  it("maps lifecycle-state keys to the state tokens, ack and closed apart", () => {
    expect(seriesColor("ack", 0)).toBe("#b083f9");
    // "close" is the wire spelling the comment plugin writes; "closed" is
    // accepted too. Both must land on the sage, not on the ok-green the
    // categorical palette used to hand out at index 1.
    expect(seriesColor("close", 1)).toBe("#84ab90");
    expect(seriesColor("closed", 1)).toBe("#84ab90");
    expect(seriesColor("snoozed", 2)).toBe("#768390");
    expect(seriesColor("shelved", 3)).toBe("#84a8cf");
    expect(seriesColor("open", 4)).toBe("#f0883e");
    expect(seriesColor("esc", 5)).toBe("#e3b341");
    expect(seriesColor("ack", 0)).not.toBe(seriesColor("close", 1));
  });

  it("falls through to the palette by index for unknown keys", () => {
    // index 0 → first palette slot (info)
    expect(seriesColor("Mystery", 0)).toBe("#58a6ff");
    // index 2 → third palette slot (warning)
    expect(seriesColor("Mystery", 2)).toBe("#e3b341");
  });
});

describe("applyChartDefaults", () => {
  afterEach(clearTokens);

  it("sets Chart.js global font family and colour from tokens", () => {
    setTokens();
    applyChartDefaults();
    expect(Chart.defaults.font.family).toBe('"IBM Plex Mono", monospace');
    expect(Chart.defaults.color).toBe("#8b96a3");
  });

  it("uses safe fallbacks when tokens are unset", () => {
    applyChartDefaults();
    expect(Chart.defaults.font.family).toBe("monospace");
    expect(Chart.defaults.color).toBe("#6b7785");
  });
});
