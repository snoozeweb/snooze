import type { BadgeVariant } from "@/shared/ui/Badge";
import { formatRelativeTime, trimDate } from "@/lib/format/time";
import type { AlertSeverity, AlertState } from "./types";

// formatRelativeTime + trimDate moved to `@/lib/format/time` so shared/ui
// primitives can use them without importing up into this feature folder.
// Re-exported here so existing alerts call sites keep their import path.
export { formatRelativeTime, trimDate };

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

function pad2(n: number): string {
  return n < 10 ? `0${n}` : String(n);
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
