// Wire helpers for the absolute-datetime family of the time-constraint editor.
// The picker works in the viewer's local wall clock; these translate between
// that and the RFC3339 wire value the Go backend stores.
//
// Emission is ZONED: composeZonedIso stamps the UTC offset computed *for the
// picked date* (via the JS Date's own getTimezoneOffset), so a summer pick
// carries +02:00 and a winter pick +01:00 for the same zone — DST-safe by
// construction. This fixes the activation bug where a zone-less
// "YYYY-MM-DDTHH:MM" value was parsed as UTC by the backend, so a window the
// operator meant for 15:00 local fired at 15:00 UTC.
//
// splitZonedIso reads a wire value back for the editor:
//   - a zoned value (…Z or …±HH:MM) denotes an absolute instant, rendered in
//     the viewer's local zone (consistent with TimeConstraintsCell, which
//     parses via `new Date(s)` too);
//   - a legacy zone-less value keeps its literal wall clock (no UTC re-drift),
//     so rules authored before this fix still display the clock they were
//     typed with.

const pad = (n: number): string => String(n).padStart(2, "0");

// hasZone reports whether an ISO string carries a timezone designator — a
// trailing Z, or a ±HH:MM offset.
function hasZone(s: string): boolean {
  return /[zZ]$/.test(s) || /[+-]\d{2}:\d{2}$/.test(s);
}

export type SplitDateTime = { date: Date | null; time: string };

// splitZonedIso turns a wire value into a calendar date + "HH:MM" for the
// editor. We pad the time to "HH:MM" (or "") so the underlying
// <input type="time"> always has a controlled value.
export function splitZonedIso(s?: string): SplitDateTime {
  if (!s) return { date: null, time: "" };
  if (hasZone(s)) {
    const d = new Date(s);
    if (!Number.isNaN(d.getTime())) {
      // Absolute instant → render in the viewer's local zone.
      return {
        date: new Date(d.getFullYear(), d.getMonth(), d.getDate()),
        time: `${pad(d.getHours())}:${pad(d.getMinutes())}`,
      };
    }
  }
  // Zone-less (legacy) or unparseable-as-zoned: read the literal wall clock so
  // the day the user typed shows without UTC drift.
  const m = s.match(/^(\d{4})-(\d{2})-(\d{2})(?:T(\d{2}):(\d{2}))?/);
  if (!m) return { date: null, time: "" };
  const y = Number(m[1]);
  const mo = Number(m[2]);
  const d = Number(m[3]);
  const time = m[4] && m[5] ? `${m[4]}:${m[5]}` : "";
  return { date: new Date(y, mo - 1, d), time };
}

// composeZonedIso combines a calendar date + "HH:MM" into a zoned RFC3339
// string in the viewer's local zone. Returns undefined when date is absent.
export function composeZonedIso(date: Date | null | undefined, time: string): string | undefined {
  if (!date) return undefined;
  const t = /^\d{2}:\d{2}$/.test(time) ? time : "00:00";
  const hh = Number(t.slice(0, 2));
  const mm = Number(t.slice(3, 5));
  // Build the instant from local components; JS applies the zone's rules for
  // THIS date, so the resulting offset is DST-correct.
  return formatOffsetIso(
    new Date(date.getFullYear(), date.getMonth(), date.getDate(), hh, mm, 0, 0),
  );
}

// formatOffsetIso renders a Date as "YYYY-MM-DDTHH:MM:SS±HH:MM" using the
// Date's own local offset. getTimezoneOffset returns minutes BEHIND UTC
// (negative east of UTC) and is evaluated for that specific date, hence
// DST-aware; we negate it for the ISO sign.
export function formatOffsetIso(d: Date): string {
  const offMin = -d.getTimezoneOffset();
  const sign = offMin >= 0 ? "+" : "-";
  const abs = Math.abs(offMin);
  return (
    `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}` +
    `T${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}` +
    `${sign}${pad(Math.floor(abs / 60))}:${pad(abs % 60)}`
  );
}
