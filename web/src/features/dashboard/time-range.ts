import type { StatsRange } from "./types";

export type TimeRange = {
  range: StatsRange;
  from: string;
  to: string;
};

export function presetToRange(
  range: StatsRange,
  now: Date = new Date(),
): { from: string; to: string } {
  if (range === "custom") return { from: "", to: "" };
  const ms: Record<Exclude<StatsRange, "custom">, number> = {
    "1d": 86_400_000,
    "1w": 7 * 86_400_000,
    "1m": 30 * 86_400_000,
    "1y": 365 * 86_400_000,
  };
  const to = now.toISOString();
  const from = new Date(now.getTime() - ms[range]).toISOString();
  return { from, to };
}

// ── ISO ↔ datetime-local string bridge ────────────────────────────────────
//
// TimeRange stores UTC ISO strings ("…Z") because that's what /stats wants.
// The shared DateTimeRangePicker (mode="datetime") speaks the native
// datetime-local wire shape "YYYY-MM-DDTHH:MM" — local clock, no seconds,
// no zone. These two helpers convert between them, matching the picker's
// own local-clock semantics so the day/time the operator sees round-trips
// without UTC drift.

/** ISO ("2026-05-14T12:30:00.000Z") → local "YYYY-MM-DDTHH:MM" (picker shape). */
export function isoToLocalInput(iso: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** Local "YYYY-MM-DDTHH:MM" (picker shape) → UTC ISO string. */
export function localInputToIso(local: string): string {
  if (!local) return "";
  const m = /^(\d{4})-(\d{2})-(\d{2})(?:T(\d{2}):(\d{2}))?/.exec(local);
  if (!m) return "";
  const d = new Date(
    Number(m[1]),
    Number(m[2]) - 1,
    Number(m[3]),
    m[4] ? Number(m[4]) : 0,
    m[5] ? Number(m[5]) : 0,
  );
  if (Number.isNaN(d.getTime())) return "";
  return d.toISOString();
}
