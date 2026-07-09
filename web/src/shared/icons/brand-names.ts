// Notifier `plugin_name`s (registry keys) that have a vendored brand glyph in
// web/public/brands.svg (Simple Icons, CC0). Keep this list in lockstep with
// the `<symbol id="brand-…">` ids in that sprite (a test enforces it). Any
// notifier not listed here (and not in MASK_BRANDS) falls back to its
// monochrome category glyph from icons.svg.
export const BRAND_NAMES = [
  "slack",
  "mattermost",
  "teams",
  "discord",
  "telegram",
  "googlechat",
  "jira",
  "pagerduty",
  "opsgenie",
  "statuspage",
  "ntfy",
  "twilio",
  "sns",
] as const;

// Brands shipped as a single-color PNG silhouette rather than a Simple Icons
// sprite symbol. BrandIcon paints these by using the PNG's alpha channel as a
// CSS mask and filling with currentColor, so they stay monochrome and
// theme-tinted exactly like the sprite glyphs. Maps `plugin_name` → public
// asset URL. The Snooze bell (logo-symbol.png, a white silhouette) marks the
// `snoozepeer` federation action (relaying alerts to another Snooze peer).
export const MASK_BRANDS = {
  snoozepeer: "/web/logo-symbol.png",
} as const;

export type SpriteBrandName = (typeof BRAND_NAMES)[number];
export type MaskBrandName = keyof typeof MASK_BRANDS;
export type BrandName = SpriteBrandName | MaskBrandName;

const SPRITE_SET: ReadonlySet<string> = new Set(BRAND_NAMES);

/**
 * Maps a notifier's `plugin_name` to its brand id, or null when no brand mark
 * (sprite or masked raster) is vendored for it. Matching is on `plugin_name` —
 * the stable registry key — not the metadata `icon` hint, which is a loose
 * label (e.g. googlechat's icon is "google", twilio's is "phone") and isn't a
 * 1:1 brand slug.
 */
export function brandFor(pluginName: string | undefined | null): BrandName | null {
  if (!pluginName) return null;
  if (SPRITE_SET.has(pluginName)) return pluginName as SpriteBrandName;
  if (pluginName in MASK_BRANDS) return pluginName as MaskBrandName;
  return null;
}

/**
 * Returns the public asset URL for a masked-raster brand (BrandIcon paints it
 * via CSS mask + currentColor), or null for sprite-backed brands rendered with
 * an `<svg><use>` reference into brands.svg.
 */
export function maskUrlFor(name: BrandName): string | null {
  return name in MASK_BRANDS ? MASK_BRANDS[name as MaskBrandName] : null;
}
