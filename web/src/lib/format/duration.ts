// parseDuration turns a short human duration string into a number of seconds.
//
// Accepted shapes are a sequence of <integer><unit> pairs with no separators,
// where unit is one of d (days), h (hours), m (minutes):
//
//   "1h"      → 3600
//   "30m"     → 1800
//   "2h30m"   → 9000
//   "1d12h"   → 129600
//   "7d"      → 604800
//
// Whitespace around the string is tolerated. The empty string, a bare number
// (no unit), an unknown unit, a zero/negative total, or any other malformed
// input returns null so the caller can show a validation message and refuse to
// mutate the form.
//
// Lives in lib/format so both feature code (snooze "Silence for…") and shared
// UI primitives (DurationInput) can share one grammar without a layering
// violation.
const SEGMENT = /(\d+)([dhm])/g;
const UNIT_SECONDS: Record<string, number> = {
  d: 86400,
  h: 3600,
  m: 60,
};

export function parseDuration(input: string): number | null {
  const s = input.trim().toLowerCase();
  if (s === "") return null;

  let total = 0;
  let matchedChars = 0;
  // Reset the stateful regex's lastIndex between calls.
  SEGMENT.lastIndex = 0;
  for (let m = SEGMENT.exec(s); m !== null; m = SEGMENT.exec(s)) {
    const value = Number.parseInt(m[1] ?? "", 10);
    const unit = m[2] ?? "";
    const seconds = UNIT_SECONDS[unit];
    if (!Number.isFinite(value) || seconds === undefined) return null;
    total += value * seconds;
    matchedChars += m[0].length;
  }

  // Reject any input with leftover characters the segment grammar did not
  // consume (e.g. "abc", "1x", "1h foo", "1.5h") — only clean unit sequences
  // are valid.
  if (matchedChars !== s.length) return null;
  if (total <= 0) return null;
  return total;
}
