// Display formatters for delivery rows. Pure functions — no React — so the
// timeline, the aria-labels it builds and the parent inspectors all print the
// same words.
import { escalationLabel } from "@/features/alerts/format";
import { severityRank } from "@/lib/format/severity-color";
import { trimDate } from "@/lib/format/time";
// Type-only: group.ts imports the formatters below at runtime, so a value
// import here would close a cycle.
import type { DeliveryGroup } from "./group";
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

/** The name a delivery is filed under: the action, or the raw notifier. */
export function deliveryActionName(row: DeliveryEntry): string {
  return row.action ?? row.notifier ?? "";
}

/**
 * A chip's tooltip: "mail-oncall · mail — Sent, Today 14:32". The row prints
 * one timestamp for the whole dispatch and one chip per action, so the
 * per-send notifier and completion time live here rather than in a column
 * that would repeat the same value down every row.
 */
export function deliveryChipTitle(row: DeliveryEntry): string {
  const name = deliveryActionName(row);
  const via = row.notifier && row.notifier !== name ? `${name} · ${row.notifier}` : name;
  const when = trimDate(row.date_epoch);
  const tail = [deliveryStatusLabel(row.status), when && when !== "—" ? when : ""]
    .filter(Boolean)
    .join(", ");
  return via ? `${via} — ${tail}` : tail;
}

/**
 * The `<li>` accessible name: "Sent via mail-oncall, 2 alerts, Today 14:32".
 * Screen readers get the whole row as one sentence instead of a badge soup.
 */
export function deliveryAriaLabel(row: DeliveryEntry): string {
  const parts: string[] = [];
  const via = deliveryActionName(row);
  parts.push(
    via ? `${deliveryStatusLabel(row.status)} via ${via}` : deliveryStatusLabel(row.status),
  );
  const n = deliveryAlertCount(row);
  if (n > 0) parts.push(`${n} ${n === 1 ? "alert" : "alerts"}`);
  const when = trimDate(row.date_epoch);
  if (when && when !== "—") parts.push(when);
  return parts.join(", ");
}

/**
 * The accessible name of a grouped row — the sentence a screen reader gets
 * instead of a strip of coloured chips:
 *
 *   "Sent via mail-oncall, slack-noc — failed via jira-ops, 1 alert, Today 14:32"
 *
 * A single-action group reduces exactly to `deliveryAriaLabel`, so a row that
 * did not need grouping still reads the way it always did.
 */
export function deliveryGroupAriaLabel(group: DeliveryGroup): string {
  const sent = group.rows.filter((r) => r.status !== "error").map(deliveryActionName);
  const failed = group.rows.filter((r) => r.status === "error").map(deliveryActionName);

  const clauses: string[] = [];
  if (sent.length > 0) clauses.push(`Sent${sent[0] ? ` via ${sent.join(", ")}` : ""}`);
  if (failed.length > 0) {
    const verb = sent.length > 0 ? "failed" : "Failed";
    clauses.push(`${verb}${failed[0] ? ` via ${failed.join(", ")}` : ""}`);
  }

  const parts = [clauses.join(" — ")];
  const n = group.alertCount;
  if (n > 0) parts.push(`${n} ${n === 1 ? "alert" : "alerts"}`);
  const when = trimDate(group.dateEpoch);
  if (when && when !== "—") parts.push(when);
  return parts.join(", ");
}

/** "Sep 1 – Sep 8" style label for the dashboard's window chip. */
export function rangeChipLabel(range: { from: number; to: number }): string {
  return `${trimDate(range.from)} – ${trimDate(range.to)}`;
}
