import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

// Parses hex custom-property declarations straight out of the theme CSS
// files and asserts WCAG contrast ratios for tokens that must stay
// perceivable against specific surfaces. This is a regression guard for
// bugs like the one that shipped --border at ~1.4:1 on --bg-surface (near
// invisible unfocused inputs) — no CSS parser, no new deps, just a regex
// over the small hex-literal subset these theme files actually use.

function parseHexTokens(css: string): Record<string, string> {
  const tokens: Record<string, string> = {};
  // Matches `--name: #rrggbb;` (also #rgb). Ignores var(...) aliases and
  // non-hex values (gradients, box-shadows, etc.) — we only need the small
  // set of solid colour tokens exercised below.
  const re = /--([a-z0-9-]+):\s*(#[0-9a-fA-F]{3,8})\s*;/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(css))) {
    const [, name, value] = m;
    if (name && value) tokens[name] = value;
  }
  return tokens;
}

function getToken(tokens: Record<string, string>, name: string): string {
  const value = tokens[name];
  expect(value, `expected --${name} to be defined`).toBeDefined();
  return value as string;
}

function hexToRgb(hex: string): [number, number, number] {
  let h = hex.slice(1);
  if (h.length === 3) {
    h = h
      .split("")
      .map((c) => c + c)
      .join("");
  }
  const num = parseInt(h.slice(0, 6), 16);
  return [(num >> 16) & 255, (num >> 8) & 255, num & 255];
}

function linearize(c: number): number {
  const s = c / 255;
  return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
}

// WCAG relative luminance (sRGB).
function relativeLuminance(rgb: [number, number, number]): number {
  const rs = linearize(rgb[0]);
  const gs = linearize(rgb[1]);
  const bs = linearize(rgb[2]);
  return 0.2126 * rs + 0.7152 * gs + 0.0722 * bs;
}

function contrastRatio(hexA: string, hexB: string): number {
  const lA = relativeLuminance(hexToRgb(hexA));
  const lB = relativeLuminance(hexToRgb(hexB));
  const lighter = Math.max(lA, lB);
  const darker = Math.min(lA, lB);
  return (lighter + 0.05) / (darker + 0.05);
}

const darkCss = readFileSync(join(__dirname, "theme.dark.css"), "utf-8");
const lightCss = readFileSync(join(__dirname, "theme.light.css"), "utf-8");

const themes = [
  { name: "dark", tokens: parseHexTokens(darkCss) },
  { name: "light", tokens: parseHexTokens(lightCss) },
];

// WCAG 1.4.11 non-text contrast minimum for UI component boundaries.
const MIN_NON_TEXT_CONTRAST = 3;

describe("theme token contrast", () => {
  it.each(themes)(
    "$name: --border-control holds >=3:1 against --bg-surface and --bg-elevated",
    ({ tokens }) => {
      const borderControl = getToken(tokens, "border-control");
      const bgSurface = getToken(tokens, "bg-surface");
      const bgElevated = getToken(tokens, "bg-elevated");

      const onSurface = contrastRatio(borderControl, bgSurface);
      const onElevated = contrastRatio(borderControl, bgElevated);

      expect(onSurface).toBeGreaterThanOrEqual(MIN_NON_TEXT_CONTRAST);
      expect(onElevated).toBeGreaterThanOrEqual(MIN_NON_TEXT_CONTRAST);
    },
  );
});
