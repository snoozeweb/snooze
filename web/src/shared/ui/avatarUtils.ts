// Pure helpers behind the Avatar primitive, kept out of Avatar.tsx so that
// file only exports components (fast refresh) and so they test on their own.

/** How many `--avatar-N` colour slots the themes define. */
export const AVATAR_TONES = 8;

/**
 * avatarTone hashes a login into one of the {@link AVATAR_TONES} colour slots
 * (1-based, to match the `--avatar-N` token names). FNV-1a over UTF-16 code
 * units: cheap, stable across sessions and browsers, and spread well enough
 * that a team of eight rarely shares a colour. Keyed by the LOGIN, not the
 * display name, so renaming someone does not repaint them.
 */
export function avatarTone(name: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < name.length; i++) {
    h ^= name.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return ((h >>> 0) % AVATAR_TONES) + 1;
}

/**
 * initialsOf picks the 1–2 letters an initials avatar shows: the first letter
 * of the first two words ("Alice Martin" → "AM", "alice.martin" → "AM"), or
 * the first letter alone for a one-word name ("alice" → "A"). Walks code
 * points, not UTF-16 units, so an accented or astral first letter survives.
 */
export function initialsOf(label: string): string {
  const words = label
    .split(/[\s._@-]+/)
    .map((w) => w.trim())
    .filter(Boolean);
  const letters = words.slice(0, 2).map((w) => Array.from(w)[0] ?? "");
  return letters.join("").toLocaleUpperCase() || "?";
}
