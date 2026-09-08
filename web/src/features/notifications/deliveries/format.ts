// Display formatters for delivery rows. Pure functions — no React — so the
// timeline, the aria-labels it builds and the parent inspectors all print the
// same words.
import { escalationLabel } from "@/features/alerts/format";
import { severityRank } from "@/lib/format/severity-color";
import { trimDate } from "@/lib/format/time";
import type { DeliveryAlert, DeliveryEntry } from "./types";

/**
 * "Sent" / "Failed". Deliberately not "success"/"error" — the row describes
 * something that happened to an operator's page, not an HTTP outcome.
 * Anything that isn't an explicit error reads as Sent (the backend only ever
 * writes the two values, and a row with no status at all was a send).
 */
export function deliveryStatusLabel(status: DeliveryEntry["status"]): string {
  return status === "error" ? "Failed" : "Sent";
}

/** Number of alerts a row covers, tolerating a row with no snapshot. */
export function deliveryAlertCount(row: DeliveryEntry): number {
  return row.alert_count ?? row.alerts?.length ?? 0;
}

/** `Batch · 7 alerts`, or null when the row is a plain unbatched send. */
export function batchLabel(row: DeliveryEntry): string | null {
  if (row.batch !== true) return null;
  const n = deliveryAlertCount(row);
  return `Batch · ${n} ${n === 1 ? "alert" : "alerts"}`;
}

/**
 * `Re-escalated x2 (timeout)`, or null for a first delivery. Reuses the
 * alerts-page formatter so the vocabulary matches the state chip on the alert
 * this delivery was for.
 */
export function escalationBadge(row: DeliveryEntry): string | null {
  return escalationLabel(row.escalation_count, row.escalation_reason);
}

// Severity ordering for the alert lines under a batched row: worst first, so
// the reason someone got paged is the first thing read. Ranks through the
// SAME ladder the badge colour beside it uses (`severityRank` consults the
// server's severity ranks before the built-in syslog fallback), so a custom
// severity an operator placed between `crit` and `err` sorts where they put
// it — a static alias table here would disagree with the colour it sits next
// to the moment anyone customises the ladder.
//
// Unknown labels have no rank at all and sort last: an unrecognised severity
// is not evidence of urgency.
const UNRANKED = Number.MAX_SAFE_INTEGER;

function severityOrder(alert: DeliveryAlert): number {
  return severityRank(alert.severity ?? "") ?? UNRANKED;
}

/**
 * sortAlertsBySeverity returns a new array ordered worst-first. Array.sort is
 * stable per spec, so alerts at the same severity keep the backend's order
 * (which is the order they were queued into the batch).
 */
export function sortAlertsBySeverity(alerts: readonly DeliveryAlert[]): DeliveryAlert[] {
  return [...alerts].sort((a, b) => severityOrder(a) - severityOrder(b));
}

/**
 * The `<li>` accessible name: "Sent via mail-oncall, 2 alerts, Today 14:32".
 * Screen readers get the whole row as one sentence instead of a badge soup.
 */
export function deliveryAriaLabel(row: DeliveryEntry): string {
  const parts: string[] = [];
  const via = row.action ?? row.notifier;
  parts.push(
    via ? `${deliveryStatusLabel(row.status)} via ${via}` : deliveryStatusLabel(row.status),
  );
  const n = deliveryAlertCount(row);
  if (n > 0) parts.push(`${n} ${n === 1 ? "alert" : "alerts"}`);
  const when = trimDate(row.date_epoch);
  if (when && when !== "—") parts.push(when);
  return parts.join(", ");
}

/** "Sep 1 – Sep 8" style label for the dashboard's window chip. */
export function rangeChipLabel(range: { from: number; to: number }): string {
  return `${trimDate(range.from)} – ${trimDate(range.to)}`;
}
