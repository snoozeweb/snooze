import type { BadgeVariant } from "@/shared/ui/Badge";
import type { AlertSeverity, AlertState } from "./types";

// Severity aliases follow the RFC 5424 / syslog ladder (emerg, alert, crit,
// err, warning, notice, info, debug) plus common upstream-monitor synonyms
// (panic, fatal, fail, ok, success). Everything at or above "crit" collapses
// to the red `critical` badge so emergencies don't render as muted.
const SEVERITY_MAP: Record<string, BadgeVariant> = {
  emerg: "critical",
  emergency: "critical",
  panic: "critical",
  alert: "critical",
  fatal: "critical",
  crit: "critical",
  critical: "critical",
  err: "error",
  error: "error",
  fail: "error",
  failure: "error",
  warn: "warning",
  warning: "warning",
  notice: "info",
  info: "info",
  informational: "info",
  ok: "ok",
  okay: "ok",
  success: "ok",
};

export function severityBadgeVariant(severity: AlertSeverity): BadgeVariant {
  return SEVERITY_MAP[severity.toLowerCase().trim()] ?? "muted";
}

const STATE_LABEL: Record<AlertState, string> = {
  // Freshly-ingested records carry an empty state until a comment moves them
  // to ack/close/esc. Display them as Open so the column reads cleanly.
  "": "Open",
  open: "Open",
  ack: "Ack",
  esc: "Escalated",
  close: "Closed",
  shelved: "Shelved",
};

// Colour palette tracks lifecycle, not urgency — the Sev column already
// signals urgency. open/empty → neutral (the default, unattended),
// ack → info (someone owns it), esc → warning (escalated, attention),
// close → closed (a muted purple, resolved), shelved → muted (deferred).
// Mirrors the legacy Vue palette in web/src/utils/api.js on origin/master.
const STATE_VARIANT: Record<AlertState, BadgeVariant> = {
  "": "neutral",
  open: "neutral",
  ack: "info",
  esc: "warning",
  close: "closed",
  shelved: "muted",
};

export function stateLabel(state: AlertState): string {
  return STATE_LABEL[state] ?? state;
}

export function stateBadgeVariant(state: AlertState): BadgeVariant {
  return STATE_VARIANT[state] ?? "neutral";
}

/**
 * formatRelativeTime emits "Xs / Xm / Xh / Xd" against now. Kept for the
 * audit timeline, comment timeline, and "last login" badge — places where
 * "8 minutes ago" reads better than the absolute trimDate format used by
 * the alerts table.
 */
export function formatRelativeTime(dateEpochSec: number | undefined): string {
  if (dateEpochSec === undefined || dateEpochSec === 0) return "—";
  const now = Math.floor(Date.now() / 1000);
  const diff = now - dateEpochSec;
  if (diff < 60) return diff <= 1 ? "just now" : `${diff}s`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h`;
  return `${Math.floor(diff / 86400)}d`;
}

const SHORT_MONTHS = [
  "Jan",
  "Feb",
  "Mar",
  "Apr",
  "May",
  "Jun",
  "Jul",
  "Aug",
  "Sep",
  "Oct",
  "Nov",
  "Dec",
] as const;

function ordinal(n: number): string {
  // English ordinal suffix: 1st, 2nd, 3rd, 4th–20th, 21st, 22nd, 23rd, 24th–30th, 31st.
  // The 11–13 carve-out is what makes 11th/12th/13th win over the units digit.
  const mod100 = n % 100;
  if (mod100 >= 11 && mod100 <= 13) return `${n}th`;
  switch (n % 10) {
    case 1:
      return `${n}st`;
    case 2:
      return `${n}nd`;
    case 3:
      return `${n}rd`;
    default:
      return `${n}th`;
  }
}

function pad2(n: number): string {
  return n < 10 ? `0${n}` : String(n);
}

/**
 * trimDate mirrors the moment.js-based formatter the old Snooze 1.x UI used
 * in web/src/utils/api.js (named "trimDate"). Same-day → "Today HH:mm",
 * different day within the current year → "MMM Do HH:mm" (e.g.
 * "Nov 5th 14:32"), different year → "MMM Do YYYY".
 *
 * The old UI exposed a `show_secs` toggle; we never used it on the alert
 * list, so the new helper sticks to minute resolution.
 */
export function trimDate(dateEpochSec: number | undefined): string {
  if (dateEpochSec === undefined || dateEpochSec === 0) return "—";
  const d = new Date(dateEpochSec * 1000);
  if (Number.isNaN(d.getTime())) return "—";
  const now = new Date();
  const hm = `${pad2(d.getHours())}:${pad2(d.getMinutes())}`;
  if (d.getFullYear() !== now.getFullYear()) {
    return `${SHORT_MONTHS[d.getMonth()]} ${ordinal(d.getDate())} ${d.getFullYear()}`;
  }
  if (d.getMonth() === now.getMonth() && d.getDate() === now.getDate()) {
    return `Today ${hm}`;
  }
  return `${SHORT_MONTHS[d.getMonth()]} ${ordinal(d.getDate())} ${hm}`;
}

/**
 * formatTTL renders an alert's TTL into a short human label suitable for a
 * table cell. The shape mirrors how the old Vue UI surfaced the same field:
 *
 *   - ttl < 0  → "shelved" (the operator soft-hid the alert)
 *   - ttl === 0 OR no ttl AND no date_epoch → "—"
 *   - expiry in the past → "expired"
 *   - expiry in the future → "in 1h 23m" (largest two units)
 *
 * The `dateEpoch` parameter is required to compute the expiry — alerts
 * expire at `date_epoch + ttl`, not "ttl seconds from now".
 */
export function formatTTL(ttl: number | undefined, dateEpochSec: number | undefined): string {
  if (ttl === undefined) return "—";
  if (ttl < 0) return "shelved";
  if (dateEpochSec === undefined || dateEpochSec === 0) return "—";
  const expiry = dateEpochSec + ttl;
  const now = Math.floor(Date.now() / 1000);
  const remaining = expiry - now;
  if (remaining <= 0) return "expired";
  return `in ${humanDuration(remaining)}`;
}

// humanDuration emits the two largest non-zero units (d / h / m / s) for a
// duration in seconds. Keeps the cell narrow without losing precision when
// the alert is about to expire ("in 12m 04s" vs "in 12m").
function humanDuration(totalSec: number): string {
  const d = Math.floor(totalSec / 86400);
  const h = Math.floor((totalSec % 86400) / 3600);
  const m = Math.floor((totalSec % 3600) / 60);
  const s = totalSec % 60;
  if (d > 0) return h > 0 ? `${d}d ${h}h` : `${d}d`;
  if (h > 0) return m > 0 ? `${h}h ${m}m` : `${h}h`;
  if (m > 0) return s > 0 ? `${m}m ${pad2(s)}s` : `${m}m`;
  return `${s}s`;
}
