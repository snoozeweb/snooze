import type { BadgeVariant } from "@/shared/ui/Badge";
import { formatRelativeTime, trimDate } from "@/lib/format/time";
import { STATE_NOUN } from "./lifecycle";
import type { AlertSeverity, AlertState } from "./types";

// Re-exported so existing call sites can keep importing the state-noun map
// from `format.ts` alongside `stateLabel`/`stateBadgeVariant` — `tabs.ts`
// imports directly from `./lifecycle` instead, to avoid a format.ts <->
// tabs.ts import cycle.
export { STATE_NOUN };

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

// Display-only title-casing for the raw syslog/monitor severity tokens.
// Storage and search both keep using the raw wire token ("err", "crit", …)
// — this only changes what the badge prints; callers should still pass the
// raw token as the badge's tooltip/title so it stays discoverable/copyable.
const SEVERITY_DISPLAY: Record<string, string> = {
  emerg: "Emergency",
  emergency: "Emergency",
  panic: "Panic",
  alert: "Alert",
  fatal: "Fatal",
  crit: "Critical",
  critical: "Critical",
  err: "Error",
  error: "Error",
  fail: "Fail",
  failure: "Failure",
  warn: "Warning",
  warning: "Warning",
  notice: "Notice",
  info: "Info",
  informational: "Informational",
  ok: "OK",
  okay: "OK",
  success: "Success",
};

export function severityDisplayLabel(severity: string): string {
  const key = severity.toLowerCase().trim();
  const known = SEVERITY_DISPLAY[key];
  if (known) return known;
  if (!severity) return severity;
  return severity.charAt(0).toUpperCase() + severity.slice(1);
}

// Canonical noun per state — same word on the chip, tab, tile, and legend.
// Freshly-ingested records carry an empty state until a comment moves them
// to ack/close/esc; display them as Open so the column reads cleanly.
// Sourced from `./lifecycle` (STATE_NOUN) so every surface stays in sync.

// Colour palette tracks lifecycle, not urgency — the Sev column already
// signals urgency. open/empty → neutral (the default, unattended),
// ack → ack (violet: a human owns it, matches the timeline/feed hue),
// esc → warning (escalated, attention), close → closed (a muted purple,
// resolved), shelved → muted (deferred).
// Mirrors the legacy Vue palette in web/src/utils/api.js on origin/master.
const STATE_VARIANT: Record<AlertState, BadgeVariant> = {
  "": "neutral",
  open: "neutral",
  ack: "ack",
  esc: "warning",
  close: "closed",
  shelved: "muted",
};

export function stateLabel(state: AlertState): string {
  return STATE_NOUN[state] ?? state;
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

/**
 * formatCountdown renders a relative "in Xh Ym" label for a future epoch
 * deadline (seconds). Returns "" when absent, zero, or already past —
 * so callers can use a falsy check to skip rendering.
 */
export function formatCountdown(epochSec: number | undefined): string {
  if (!epochSec) return "";
  const remaining = epochSec - Math.floor(Date.now() / 1000);
  if (remaining <= 0) return "";
  return `in ${humanDuration(remaining)}`;
}

/**
 * formatShelveUntil renders the timed-shelve countdown for records where
 * state=="shelved" and shelve_until > 0.
 *
 * Returns "" when shelve_until is absent or zero — caller falls through to
 * formatTTL for legacy ttl<0 permanent-shelved rows.
 */
export function formatShelveUntil(shelveUntilEpoch: number | undefined): string {
  if (!shelveUntilEpoch) return "";
  const now = Math.floor(Date.now() / 1000);
  const remaining = shelveUntilEpoch - now;
  if (remaining <= 0) return "expired (pending sweep)";
  return `returns in ${humanDuration(remaining)}`;
}

/**
 * stateDeadline is when the alert's current state ends on its own, for the
 * state badge: an acknowledgement with an `ack_until` reopens ("reopens in
 * 2h"), a timed shelve returns ("returns in 30m"). "" for a state with no
 * deadline. Past the deadline — the housekeeper's sweep has not run yet — it
 * says "shortly" rather than a negative time.
 */
export function stateDeadline(r: {
  state?: string | undefined;
  ack_until?: unknown;
  shelve_until?: unknown;
}): string {
  const deadline = (v: unknown) => (typeof v === "number" && v > 0 ? v : 0);
  const verb = r.state === "ack" ? "reopens" : r.state === "shelved" ? "returns" : "";
  const until =
    r.state === "ack"
      ? deadline(r.ack_until)
      : r.state === "shelved"
        ? deadline(r.shelve_until)
        : 0;
  if (!verb || !until) return "";
  const remaining = until - Math.floor(Date.now() / 1000);
  return remaining > 0 ? `${verb} in ${humanDuration(remaining)}` : `${verb} shortly`;
}

const TREND_LABEL: Record<string, string> = {
  moreSevere: "Severity escalated",
  lessSevere: "Severity decreased",
  noChange: "No change",
};

export function trendLabel(trend: string): string {
  return TREND_LABEL[trend] ?? "";
}

// humanDuration emits the two largest non-zero units (d / h / m / s) for a
// duration in seconds. Keeps the cell narrow without losing precision when
// the alert is about to expire ("in 12m 04s" vs "in 12m").
export function humanDuration(totalSec: number): string {
  const d = Math.floor(totalSec / 86400);
  const h = Math.floor((totalSec % 86400) / 3600);
  const m = Math.floor((totalSec % 3600) / 60);
  const s = totalSec % 60;
  if (d > 0) return h > 0 ? `${d}d ${h}h` : `${d}d`;
  if (h > 0) return m > 0 ? `${h}h ${m}m` : `${h}h`;
  if (m > 0) return s > 0 ? `${m}m ${pad2(s)}s` : `${m}m`;
  return `${s}s`;
}

/**
 * escalationLabel renders the escalation summary shown beside an alert's state:
 * "Re-escalated x3 (timeout)", or null when the alert has never been
 * re-escalated.
 *
 * Deliberately shares the "Re-escalated" noun with the state chip (state=esc
 * — see STATE_NOUN in `./lifecycle`): the canonical vocabulary uses one word
 * per lifecycle fact everywhere, so the two badges are allowed to agree on
 * the headline. This one adds the qualifier the chip has no room for: how
 * many times and why.
 *
 * Null rather than an empty string so a caller renders nothing at all for the
 * common case — a first-delivery alert should carry no extra chrome.
 */
export function escalationLabel(
  count: number | undefined,
  reason?: string,
  actor?: string,
): string | null {
  if (!count || count < 1) return null;
  let label = count === 1 ? "Re-escalated" : `Re-escalated x${count}`;
  if (reason === "manual" && actor) {
    // Attribution matters more than the reason word for a manual escalation:
    // "who did this" is the question an operator asks next.
    label += ` by ${actor}`;
  } else if (reason) {
    label += ` (${reason})`;
  }
  return label;
}
