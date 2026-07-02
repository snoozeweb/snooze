import type { TimeConstraintsGroup } from "./types";

/**
 * timeConstraintsError returns a human-readable error when any absolute-date or
 * time-of-day window is incomplete (missing its start or end), else null.
 *
 * Why: an entry like { from: "", until: "" } — what the editor's "Add range"
 * button seeds — can never match. The backend's classify() then treats the
 * whole snooze as always-on, so the UI shows "always on" for a snooze that in
 * fact matches nothing. Blocking save on a fully-empty window closes that trap.
 *
 * A ONE-SIDED range is legitimate, not an error: the backend Match supports a
 * half-open ray (from-only = "t >= from"; until-only = "t <= until"), and the
 * date-range picker emits exactly that on a single-ended pick. Only a range
 * with BOTH bounds empty is the vacuous never-matching shape. An empty group
 * (no windows at all) is legitimately always-on and is allowed.
 */
export function timeConstraintsError(g: TimeConstraintsGroup): string | null {
  const bothEmpty = (r: { from?: string; until?: string }) =>
    !(r.from ?? "").trim() && !(r.until ?? "").trim();
  if ((g.datetime ?? []).some(bothEmpty)) {
    return "Every absolute date range needs a start or an end. Fill it in or remove it.";
  }
  if ((g.time ?? []).some(bothEmpty)) {
    return "Every time-of-day window needs a start or an end. Fill it in or remove it.";
  }
  return null;
}
