// Maps a raw severity string to a concrete hex colour for charts, matching the
// alert-page badge palette (the --severity-* theme tokens). When several raw
// labels collapse to the same variant, they are tinted apart by syslog rank:
// MORE serious than the variant's canonical label → darker; less serious →
// lighter. Option X: `error` keeps its own orange variant (not folded into red).

type Variant = "critical" | "error" | "warning" | "info" | "ok" | "muted";

const RANK: Record<string, number> = {
  emerg: 0, emergency: 0, panic: 0,
  alert: 1,
  crit: 2, critical: 2, fatal: 2,
  err: 3, error: 3, fail: 3, failure: 3,
  warn: 4, warning: 4,
  notice: 5,
  info: 6, informational: 6,
  debug: 7,
  ok: 8, okay: 8, success: 8,
};

function variantOf(label: string): Variant {
  const r = RANK[label.toLowerCase().trim()];
  if (r === undefined) return "muted";
  if (r <= 2) return "critical";
  if (r === 3) return "error";
  if (r === 4) return "warning";
  if (r === 5 || r === 6 || r === 7) return "info";
  return "ok";
}

const CANONICAL_RANK: Record<Variant, number> = {
  critical: 2, error: 3, warning: 4, info: 6, ok: 8, muted: -1,
};

const TOKEN: Record<Variant, string> = {
  critical: "--severity-critical",
  error: "--severity-error",
  warning: "--severity-warning",
  info: "--severity-info",
  ok: "--severity-ok",
  muted: "--text-muted",
};

const STEP = 0.14;

export function severityColor(label: string): string {
  const variant = variantOf(label);
  const base = readToken(TOKEN[variant]) || "#6b7785";
  if (variant === "muted") return base;
  const rank = RANK[label.toLowerCase().trim()] ?? CANONICAL_RANK[variant];
  const shift = CANONICAL_RANK[variant] - rank;
  if (shift === 0) return base;
  return shift > 0 ? darken(base, shift * STEP) : lighten(base, -shift * STEP);
}

/** Build a label→colour map for a DonutChart `colors` prop. */
export function severityColors(labels: string[]): Record<string, string> {
  return Object.fromEntries(labels.map((l) => [l, severityColor(l)]));
}

function readToken(name: string): string {
  if (typeof document === "undefined") return "";
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return v.startsWith("#") ? v : v ? toHex(v) : "";
}

function hexToRgb(hex: string): [number, number, number] {
  const h = hex.replace("#", "");
  return [parseInt(h.slice(0, 2), 16), parseInt(h.slice(2, 4), 16), parseInt(h.slice(4, 6), 16)];
}
function rgbToHex(r: number, g: number, b: number): string {
  const c = (n: number) => Math.max(0, Math.min(255, Math.round(n))).toString(16).padStart(2, "0");
  return `#${c(r)}${c(g)}${c(b)}`;
}
function darken(hex: string, t: number): string {
  const [r, g, b] = hexToRgb(hex);
  return rgbToHex(r * (1 - t), g * (1 - t), b * (1 - t));
}
function lighten(hex: string, t: number): string {
  const [r, g, b] = hexToRgb(hex);
  return rgbToHex(r + (255 - r) * t, g + (255 - g) * t, b + (255 - b) * t);
}
function toHex(v: string): string {
  const m = v.match(/(\d+)[,\s]+(\d+)[,\s]+(\d+)/);
  return m ? rgbToHex(+m[1]!, +m[2]!, +m[3]!) : "";
}
