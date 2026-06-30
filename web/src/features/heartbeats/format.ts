/**
 * parsedLastSeenEpoch — parses a RFC3339 last_seen string into epoch seconds.
 * Returns undefined when the input is absent or not a valid date.
 */
export function parsedLastSeenEpoch(last_seen: string | undefined): number | undefined {
  if (!last_seen) return undefined;
  const ms = Date.parse(last_seen);
  if (isNaN(ms)) return undefined;
  return Math.floor(ms / 1000);
}
