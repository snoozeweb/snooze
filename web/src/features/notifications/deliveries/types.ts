// Types for the delivery history (the `notificationlog` collection).
//
// Terminology (plan Finding 15): in Snooze a *notification* is the routing
// object that decides WHEN to send. One actual send is a **delivery**. Never
// call a row here "a notification" — the two-tab concept strip on the
// notifications page depends on that word staying free.
import type { components } from "@/lib/api/types.gen";

/** One row of the delivery log — a single send attempt by one action. */
export type DeliveryEntry = components["schemas"]["NotificationLogEntry"];

/**
 * One alert snapshot carried by a delivery row. Captured at send time, so it
 * still renders after the alert record itself has expired (2 day record TTL
 * vs. 30 day log retention).
 */
export type DeliveryAlert = NonNullable<DeliveryEntry["alerts"]>[number];

/** Which object's deliveries we are listing. */
export type DeliveryScope =
  | { kind: "notification"; uid: string }
  | { kind: "action"; name: string }
  | { kind: "alert"; uid: string };

/** Inclusive epoch-second window (the dashboard deep link's chart range). */
export type DeliveryRange = { from: number; to: number };

/**
 * A scope plus the optional narrowing the timeline's chips and the dashboard
 * deep link apply. Serialised into the query key verbatim, so keep it flat
 * and JSON-stable.
 */
export type DeliveryFilter = DeliveryScope & {
  /** Only failed deliveries. `"error"` is the only status worth filtering on. */
  status?: "error";
  /** Only batched flushes. */
  batchOnly?: boolean;
  /** Restrict to a `date_epoch` window. */
  range?: DeliveryRange;
  /** Only rows a given notification (uid) routed — a folded run's members. */
  notification?: string;
};

/** One folded run of repeats (GET /notificationlog/runs). */
export type DeliveryRun = components["schemas"]["DeliveryRun"];
/** A page of runs plus the whole alert's summary. */
export type DeliveryRunsResponse = components["schemas"]["DeliveryRunsResponse"];

/** Which columns a `DeliveryTimeline` renders — set by the hosting inspector. */
export type DeliveryVariant = "notification" | "action" | "alert";
