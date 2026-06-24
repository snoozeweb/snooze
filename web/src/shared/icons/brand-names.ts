// Notifier `plugin_name`s (registry keys) that have a vendored brand glyph in
// web/public/brands.svg (Simple Icons, CC0). Keep this list in lockstep with
// the `<symbol id="brand-…">` ids in that sprite. Any notifier not listed here
// falls back to its monochrome category glyph from icons.svg.
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

export type BrandName = (typeof BRAND_NAMES)[number];

const BRAND_SET: ReadonlySet<string> = new Set(BRAND_NAMES);

/**
 * Maps a notifier's `plugin_name` to its brand glyph id, or null when no brand
 * logo is vendored for it. Matching is on `plugin_name` — the stable registry
 * key — not the metadata `icon` hint, which is a loose label (e.g. googlechat's
 * icon is "google", twilio's is "phone") and isn't a 1:1 brand slug.
 */
export function brandFor(pluginName: string | undefined | null): BrandName | null {
  return pluginName && BRAND_SET.has(pluginName) ? (pluginName as BrandName) : null;
}
