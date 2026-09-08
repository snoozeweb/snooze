// Pure formatting for the alert inspector's "Last notified …" summary line —
// see AlertRowDetail. Kept out of the component so the success/failure/batch
// wording is unit-testable without mounting React or MSW.
import type { DeliveryEntry } from "@/features/notifications/deliveries/types";

export type LastDeliverySummary = {
  /** Drives the header line's copy and color. */
  variant: "success" | "error";
  /** `date_epoch` of the newest delivery — fed straight to `TimeCell`. */
  epoch: number;
  /** Action name, falling back to the notifier key when the action is unset. */
  via: string;
  /** Alert count when the row was a batch flush, else null. */
  batchCount: number | null;
};

/**
 * lastDeliverySummary reduces the newest delivery row (or none) to the
 * header-line shape. Returns null when there is nothing to show — the caller
 * renders no line at all rather than a skeleton, so the header never shifts.
 */
export function lastDeliverySummary(row: DeliveryEntry | undefined): LastDeliverySummary | null {
  if (!row || row.date_epoch === undefined) return null;
  const via = row.action ?? row.notifier ?? "";
  const batch = row.batch === true;
  const batchCount = batch ? (row.alert_count ?? row.alerts?.length ?? null) : null;
  return {
    variant: row.status === "error" ? "error" : "success",
    epoch: row.date_epoch,
    via,
    batchCount,
  };
}
