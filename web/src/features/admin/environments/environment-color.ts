// A small fixed palette of distinct, readable hues used as a stable fallback
// colour for environments that have no explicit colour set. Keeps first-run
// environments visually distinguishable on the Alerts environment bar instead
// of all rendering as one accent-coloured (or black) pill.
// Severity red and the brand amber are deliberately absent: an environment
// that defaults to #ef4444 reads as a critical alert, and one that defaults
// to #f59e0b reads as a primary button. Eight spread hues remain, none of
// which is the first thing an operator's eye is trained to read as danger.
const ENV_PALETTE = [
  "#4f8cff",
  "#22c55e",
  "#0891b2",
  "#a855f7",
  "#6366f1",
  "#14b8a6",
  "#ec4899",
  "#84cc16",
];

/**
 * envColor maps an environment name to a stable palette colour (same name →
 * same colour across sessions and members), so an environment saved without an
 * explicit colour is still distinguishable rather than a solid black pill.
 */
export function envColor(name: string): string {
  let h = 0;
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) | 0;
  return ENV_PALETTE[Math.abs(h) % ENV_PALETTE.length]!;
}
