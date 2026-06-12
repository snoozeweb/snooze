// Chart theming helpers — the single source of truth for the colours and
// typography our Chart.js wrappers (LineChart/BarChart) and the pure-CSS
// DistributionBar read at render time.
//
// Charts can't use CSS custom properties directly (Chart.js paints to a
// canvas, which doesn't resolve `var(--x)`), so we resolve the tokens to
// concrete strings via getComputedStyle. This generalises the private
// `cssVar()` helpers that used to live in LineChart/BarChart and the
// `readToken` pattern in src/lib/format/severity-color.ts.
//
// All functions degrade gracefully under SSR / jsdom, where
// getComputedStyle returns an empty string for unset custom properties:
// they fall back to a hard-coded neutral so charts still render in tests.

import { Chart } from "chart.js";

/** Neutral fallback when a token is unresolved (SSR, jsdom, typo). */
const FALLBACK = "#6b7785";

/**
 * Resolve a CSS custom property to its concrete value off
 * `document.documentElement`. Returns `fallback` when there's no DOM or the
 * property is unset/empty (the jsdom case the unit tests exercise).
 */
export function chartToken(name: string, fallback: string = FALLBACK): string {
  if (typeof document === "undefined") return fallback;
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return v || fallback;
}

// Ordered categorical palette. Kept token-driven so it re-themes with
// light/dark; the order is deliberate (cool→warm, ending on neutrals) so
// adjacent series stay distinguishable on the line/bar charts.
const PALETTE_TOKENS = [
  "--severity-info", // blue
  "--severity-ok", // green
  "--severity-warning", // gold
  "--severity-error", // orange
  "--severity-critical", // red
  "--state-ack", // purple
  "--state-shelve", // steel blue
  "--state-snooze", // grey
] as const;

/** Ordered categorical palette, resolved to concrete colours. */
export function chartPalette(): string[] {
  return PALETTE_TOKENS.map((t) => chartToken(t));
}

// The dashboard's named time-series keys (must match the backend /stats
// `series[].counts` keys) mapped to a semantic token. A key without an
// entry here falls through to the categorical palette by index.
const SERIES_TOKEN: Record<string, string> = {
  Alerts: "--severity-info",
  Throttled: "--state-ack",
  Snoozed: "--severity-warning",
  "Notification sent": "--severity-ok",
  "Action error": "--severity-critical",
  // Bar-panel series the dashboard reuses:
  Successful: "--severity-ok",
  Failed: "--severity-critical",
  Hosts: "--severity-info",
};

/**
 * Colour for a named dashboard series. Falls back to the categorical
 * palette (keyed by `index`) when the series name isn't recognised, and to
 * the neutral when even that is unavailable.
 */
export function seriesColor(key: string, index = 0): string {
  const token = SERIES_TOKEN[key];
  if (token) return chartToken(token);
  const palette = chartPalette();
  return palette[index % palette.length] ?? FALLBACK;
}

// applyChartDefaults is idempotent and cheap; the wrappers call it once
// before constructing a chart so axis/legend/tooltip text picks up our
// mono font and muted colour without per-chart config noise.
//
// Guarded against a Chart stub lacking `defaults` (the unit tests mock
// chart.js with a minimal class) so calling it there is a harmless no-op.
export function applyChartDefaults(): void {
  const defaults = (Chart as { defaults?: { font?: { family?: string }; color?: string } })
    .defaults;
  if (!defaults?.font) return;
  defaults.font.family = chartToken("--font-mono", "monospace");
  defaults.color = chartToken("--text-muted");
}
