// Compute a snooze's lifecycle tab from its time_constraints. Mirrors the
// legacy Vue UI (web/src/views/Snooze.vue:get_tabs_default on
// origin/master) and ports its predicate exactly:
//
//   expired  — every DateTime.until is in the past
//   upcoming — every DateTime.from is in the future (and not expired)
//   active   — everything else (including snoozes with no datetime
//              constraint, which the legacy UI considered "always-on")
//
// We only look at the DateTime family. Weekdays / daily-time windows are
// orthogonal — a snooze active "Mon-Fri 09:00-17:00" never *expires*; it
// goes inactive between business hours but stays in the Active tab.
import type { Snooze } from "./types";

export type SnoozeState = "active" | "upcoming" | "expired";

function epochOf(iso: string | undefined): number | undefined {
  if (!iso) return undefined;
  const ms = Date.parse(iso);
  return Number.isFinite(ms) ? Math.floor(ms / 1000) : undefined;
}

export function snoozeState(s: Snooze, nowEpoch = Math.floor(Date.now() / 1000)): SnoozeState {
  const ranges = s.time_constraints?.datetime ?? [];
  if (ranges.length === 0) return "active";

  // Expired iff every range's `until` exists and is in the past.
  const allExpired = ranges.every((r) => {
    const u = epochOf(r.until);
    return u !== undefined && u < nowEpoch;
  });
  if (allExpired) return "expired";

  // Upcoming iff every range's `from` exists and is in the future.
  const allUpcoming = ranges.every((r) => {
    const f = epochOf(r.from);
    return f !== undefined && f > nowEpoch;
  });
  if (allUpcoming) return "upcoming";

  return "active";
}
