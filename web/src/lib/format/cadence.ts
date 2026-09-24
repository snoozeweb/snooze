// How often something repeats, said the way an operator would say it:
// "every ~16 min". Used where a run of identical events is folded into one
// line (the alert inspector's Deliveries and Timeline tabs).

const MINUTE = 60;
const HOUR = 3600;
const DAY = 86400;

/**
 * cadenceLabel turns a median gap in seconds into "every ~16 min", or "" when
 * there is no gap to speak of (a run of one). One unit, rounded: the value is
 * a median of real-world gaps, and "every ~16 min 3 s" would claim a precision
 * it does not have.
 */
export function cadenceLabel(seconds: number | undefined): string {
  if (!seconds || seconds <= 0 || !Number.isFinite(seconds)) return "";
  if (seconds < MINUTE) return `every ~${Math.round(seconds)} s`;
  if (seconds < HOUR) return `every ~${Math.max(1, Math.round(seconds / MINUTE))} min`;
  if (seconds < DAY) return `every ~${Math.round(seconds / HOUR)} h`;
  return `every ~${Math.round(seconds / DAY)} d`;
}

/** "×1,420" — how many times a folded run repeated. */
export function repeatLabel(count: number): string {
  return `×${count.toLocaleString("en-US")}`;
}
