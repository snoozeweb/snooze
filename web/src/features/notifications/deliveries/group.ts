// Grouping the delivery log into one visual row per *dispatch*.
//
// The backend writes one row per (alert × action): a notification with three
// actions produces three rows, at the same second, for the same alert. Listed
// raw that reads as three separate pages, and the operator has to reconstruct
// "this notification fired, and here is how each of its actions did" by eye.
//
// So the timeline groups. One group = one notification firing once for one set
// of alerts; its actions are chips inside the row, each carrying its own
// outcome. The header count above the list still counts *deliveries* (raw
// sends) — that number is the log's own, and grouping must not silently
// deflate it.
//
// The grouping key
// ----------------
// `queued_epoch` is stamped ONCE per record dispatch on the request goroutine
// (internal/pluginimpl/notification/plugin.go), so every action fired by the
// same dispatch shares it exactly — it is the join key the log was already
// carrying, not a heuristic on `date_epoch` (which is per-send completion and
// differs by milliseconds → seconds across the fan-out).
//
// It is not sufficient alone: it is per *record*, not per notification, so two
// notifications routing the same alert share it. The key therefore adds:
//
//   - the notification identity, so two routes never collapse into one row;
//   - the alert set, so a batched flush never merges with an unbatched send
//     that happened to be queued in the same second;
//   - the escalation count, so a re-escalation is always its own row;
//   - the batch flag, for the same reason as the alert set.
//
// Grouping is per PAGE, deliberately: it re-shapes what the operator is
// looking at without changing what was fetched, so paging, the chips and the
// counts keep their existing (server-side, row-level) meaning. A dispatch
// whose actions straddle a page boundary shows as two partial rows — the same
// truthful split the underlying pagination already imposes.
import { batchLabel, deliveryAlertCount, escalationBadge } from "./format";
import type { DeliveryAlert, DeliveryEntry } from "./types";

/** One dispatch: the sends that left together, newest completion first. */
export type DeliveryGroup = {
  /** Stable React key — the grouping key itself. */
  key: string;
  /** Members, ordered for display by `chipOrder`: failures first, then name. */
  rows: DeliveryEntry[];
  /** Newest completion in the group; what the row prints and sorts on. */
  dateEpoch: number | undefined;
  /** Notifications that routed this dispatch (union, first-seen order). */
  notifications: string[];
  /** The alert snapshot the dispatch covered — shared by every member. */
  alerts: DeliveryAlert[];
  /** Alerts covered, from the member that knows the most about them. */
  alertCount: number;
  /** How many member sends failed. 0 means the whole dispatch landed. */
  failed: number;
  /** `Batch · 7 alerts`, or null when nothing in the group was batched. */
  batch: string | null;
  /** `Re-escalated x2 (timeout)`, or null on a first delivery. */
  escalation: string | null;
};

function joinSorted(values: readonly string[] | undefined): string {
  if (!values || values.length === 0) return "";
  return [...values].sort().join("");
}

/** The notification identity of a row: uids when resolved, else names. */
function notificationKey(row: DeliveryEntry): string {
  return joinSorted(row.notification_uids) || joinSorted(row.notification_names);
}

/**
 * The alert-set identity. Hashes first (always written by the backend and
 * stable across a re-occurrence), uids as a fallback, and finally the count —
 * which at least keeps a snapshot-less row from merging with a different one.
 */
function alertKey(row: DeliveryEntry): string {
  const hashes = joinSorted(row.alert_hashes);
  if (hashes) return `h:${hashes}`;
  const uids = joinSorted(row.alert_uids);
  if (uids) return `u:${uids}`;
  return `n:${deliveryAlertCount(row)}`;
}

function groupKey(row: DeliveryEntry): string {
  const when = row.queued_epoch ?? row.date_epoch ?? 0;
  const esc = `${row.escalation_count ?? 0}/${row.escalation_reason ?? ""}`;
  return [notificationKey(row), when, esc, row.batch === true ? "b" : "", alertKey(row)].join("|");
}

/**
 * groupDeliveries collapses a page of delivery rows into dispatch groups,
 * preserving the server's newest-first order: the list arrives sorted by
 * `date_epoch` desc, and a group takes the position of its first (newest)
 * member, so the column of timestamps stays monotone.
 */
export function groupDeliveries(rows: readonly DeliveryEntry[]): DeliveryGroup[] {
  const byKey = new Map<string, DeliveryGroup>();

  for (const row of rows) {
    const key = groupKey(row);
    let group = byKey.get(key);
    if (!group) {
      group = {
        key,
        rows: [],
        dateEpoch: undefined,
        notifications: [],
        alerts: [],
        alertCount: 0,
        failed: 0,
        batch: null,
        escalation: null,
      };
      byKey.set(key, group);
    }

    group.rows.push(row);
    if (row.status === "error") group.failed += 1;
    // Newest member wins the printed time. The list is already desc-sorted,
    // so this is normally the first member — but a page whose order the
    // server ever changes must not silently print an older timestamp.
    if (group.dateEpoch === undefined || (row.date_epoch ?? 0) > group.dateEpoch) {
      group.dateEpoch = row.date_epoch ?? group.dateEpoch;
    }
    for (const name of row.notification_names ?? []) {
      if (name && !group.notifications.includes(name)) group.notifications.push(name);
    }
    // Members share the alert set by construction of the key; take the
    // richest snapshot in case one member's was dropped.
    const alerts = row.alerts ?? [];
    if (alerts.length > group.alerts.length) group.alerts = alerts;
    group.alertCount = Math.max(group.alertCount, deliveryAlertCount(row));
    group.batch ??= batchLabel(row);
    group.escalation ??= escalationBadge(row);
  }

  for (const group of byKey.values()) group.rows.sort(chipOrder);
  return [...byKey.values()];
}

/**
 * Chip order inside a row. The server returns the fan-out in completion
 * order, which is a race — the same three actions come back in a different
 * order on every dispatch, and a column of chips that reshuffles between rows
 * defeats the alignment it sits in. So: failures first (the row's reason for
 * existing), then by name. An action that keeps failing therefore keeps its
 * position down the list, and a failure is never buried behind five greens.
 */
function chipOrder(a: DeliveryEntry, b: DeliveryEntry): number {
  const af = a.status === "error" ? 0 : 1;
  const bf = b.status === "error" ? 0 : 1;
  if (af !== bf) return af - bf;
  return (a.action ?? a.notifier ?? "").localeCompare(b.action ?? b.notifier ?? "");
}
