// TanStack Query hooks over GET /api/v1/notificationlog — the delivery log.
//
// The collection is a plain data-model plugin, so it gets the standard CRUD
// list surface: `?q=<base64url condition>&orderby&asc&limit&offset` returning
// `{ data, meta:{ total } }`. We hand-roll the hooks instead of using
// defineResource() because every call here is a filtered, newest-first list
// and the query key has to carry the filter, not an opaque params blob.
import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { api, type ApiError } from "@/lib/api/client";
import type { ListResponse } from "@/lib/api/resource";
import { encodeConditionQ } from "@/lib/condition/serialize";
import type { Condition } from "@/lib/condition/types";
import type { DeliveryEntry, DeliveryFilter } from "./types";

/** REST path (relative to /api/v1) of the delivery-log collection. */
export const DELIVERY_PATH = "/notificationlog";

/** Query-key namespace. Task 11+ invalidate against this. */
export const DELIVERY_QUERY_KEY = "notificationlog";

/** Live-refresh cadence used when the hosting drawer is open. */
export const DELIVERY_REFETCH_MS = 30_000;

export type DeliveryPage = { limit: number; offset: number };

export type DeliveryQueryOptions = {
  /** Poll every 30 s. Pass true only while the hosting drawer is open. */
  live?: boolean;
  /**
   * Gate the request. Defaults to true. Callers pass false when the perm is
   * missing (so a read-only role never fires a 403 probe) or when the scope
   * has no identifier yet.
   */
  enabled?: boolean;
};

/**
 * Every scope narrows on ONE identifier, and an empty one is not a wider
 * query — it is a catastrophically wider one. The server evaluates CONTAINS as
 * a regex, so `alert_uids CONTAINS ""` matches every row in the tenant. The
 * hooks refuse to fire without an identifier and `buildDeliveryCondition`
 * throws, so the two guards cannot drift apart.
 */
export function deliveryScopeValue(filter: DeliveryFilter): string {
  return filter.kind === "action" ? filter.name : filter.uid;
}

/**
 * Stable identity of the *object* a query is about, independent of the chips,
 * paging and window layered on top. Query keys carry it as their own segment
 * so `placeholderData` can tell "next page of the same notification" (keep the
 * rows on screen) from "a different notification" (drop them — showing another
 * object's history under this one's header is a lie, not a loading state).
 */
export function deliveryScopeKey(filter: DeliveryFilter): string {
  return `${filter.kind}:${deliveryScopeValue(filter)}`;
}

/** The narrowing layered on the scope — chips and the dashboard's window. */
function deliveryNarrowKey(filter: DeliveryFilter): string {
  return JSON.stringify({
    status: filter.status ?? null,
    batchOnly: filter.batchOnly ?? false,
    range: filter.range ?? null,
  });
}

function deliveryQueryKey(filter: DeliveryFilter, page: DeliveryPage): unknown[] {
  return [
    DELIVERY_QUERY_KEY,
    deliveryScopeKey(filter),
    deliveryNarrowKey(filter),
    page.limit,
    page.offset,
  ];
}

/**
 * buildDeliveryCondition lowers a filter into the AND condition sent as `?q=`.
 *
 * Scope clauses use the flat `*_uids` arrays on the row (CONTAINS matches a
 * JSON array member on every driver) rather than object-path filtering, which
 * the DSL cannot express.
 *
 * Always returns an AND — even for a single clause — so the shape is stable
 * for tests and for anything that wants to append to `args`.
 *
 * @throws when the scope carries no identifier (see `deliveryScopeValue`).
 */
export function buildDeliveryCondition(filter: DeliveryFilter): Condition {
  const scopeValue = deliveryScopeValue(filter);
  if (scopeValue === "") {
    throw new Error(
      `delivery scope "${filter.kind}" has no identifier — an empty CONTAINS matches the whole tenant`,
    );
  }
  const args: Condition[] = [];
  switch (filter.kind) {
    case "notification":
      args.push({ type: "CONTAINS", field: "notification_uids", value: scopeValue });
      break;
    case "action":
      args.push({ type: "EQUALS", field: "action", value: scopeValue });
      break;
    case "alert":
      args.push({ type: "CONTAINS", field: "alert_uids", value: scopeValue });
      break;
  }
  if (filter.status !== undefined) {
    args.push({ type: "EQUALS", field: "status", value: filter.status });
  }
  if (filter.batchOnly === true) {
    // A real JSON boolean, not the string "true": every driver compares by
    // type (SQLite binds a Go bool to 1, Mongo matches a BSON bool), so the
    // string would silently match nothing. The DSL's `batch = true` parses to
    // exactly this node — see lib/condition/types.ts `LeafBoolOp`.
    args.push({ type: "EQUALS", field: "batch", value: true });
  }
  if (filter.range !== undefined) {
    args.push({ type: "GE", field: "date_epoch", value: filter.range.from });
    args.push({ type: "LE", field: "date_epoch", value: filter.range.to });
  }
  return { type: "AND", args };
}

/**
 * useDeliveries lists delivery rows for `filter`, newest first.
 *
 * `opts.live` turns on a 30 s refetch — pass it only while the hosting drawer
 * is open, so a closed inspector costs nothing.
 *
 * Previous data is kept only *within one scope*, so paging and chip changes
 * don't flash back to skeletons while retargeting the docked drawer at another
 * notification clears the list immediately. Callers dim the list on
 * `isPlaceholderData` so a stale page is never mistaken for a fresh one.
 */
export function useDeliveries(
  filter: DeliveryFilter,
  page: DeliveryPage,
  opts?: DeliveryQueryOptions,
): UseQueryResult<ListResponse<DeliveryEntry>, ApiError> {
  const { limit, offset } = page;
  const scope = deliveryScopeKey(filter);
  const enabled = (opts?.enabled ?? true) && deliveryScopeValue(filter) !== "";
  return useQuery<ListResponse<DeliveryEntry>, ApiError>({
    queryKey: deliveryQueryKey(filter, page),
    // The condition is built inside the queryFn on purpose: it throws on an
    // empty scope, and a disabled query must not throw during render.
    queryFn: ({ signal }) =>
      api<ListResponse<DeliveryEntry>>("GET", DELIVERY_PATH, {
        query: {
          q: encodeConditionQ(buildDeliveryCondition(filter)),
          orderby: "date_epoch",
          asc: false,
          limit,
          offset,
        },
        signal,
      }),
    enabled,
    placeholderData: (prev, prevQuery) => (prevQuery?.queryKey?.[1] === scope ? prev : undefined),
    ...(opts?.live === true ? { refetchInterval: DELIVERY_REFETCH_MS } : {}),
  });
}

// One row is enough when all we want is meta.total — but that one row is also
// the *newest* one, which is exactly what the alert inspector's "Last notified"
// line needs. Shared constant so every caller lands on the same query key and
// dedupes: the tab-count badge and the header line are one request, not two.
const SUMMARY_PAGE: DeliveryPage = { limit: 1, offset: 0 };

export type DeliverySummary = {
  /** Total rows matching the filter, or undefined while loading. */
  total: number | undefined;
  /** The newest matching row, or undefined when there is none. */
  latest: DeliveryEntry | undefined;
  isPending: boolean;
};

/**
 * useDeliverySummary answers "how many, and what was the last one?" in a
 * single `limit=1` request.
 *
 * Both facts come off the same response (`meta.total` and `data[0]`), so the
 * alert inspector's tab-count badge and its "Last notified …" summary line
 * cost ONE round trip between them — the header line and the badge share the
 * key and dedupe.
 */
export function useDeliverySummary(
  filter: DeliveryFilter,
  opts?: DeliveryQueryOptions,
): DeliverySummary {
  const query = useDeliveries(filter, SUMMARY_PAGE, opts);
  return {
    total: query.data?.meta.total,
    latest: query.data?.data[0],
    isPending: query.isPending,
  };
}

export type DeliveryFailedCount = {
  /** How many of the scope's deliveries failed, or undefined while loading. */
  failed: number | undefined;
  isPending: boolean;
};

/**
 * useFailedDeliveryCount feeds the "· 3 failed" half of the timeline header.
 *
 * It is the only count query the timeline pays for: the total comes off the
 * list query the timeline already runs (same page of rows, same `meta.total`),
 * so the header costs one extra `limit=1` call rather than two.
 */
export function useFailedDeliveryCount(
  filter: DeliveryFilter,
  opts?: DeliveryQueryOptions,
): DeliveryFailedCount {
  const query = useDeliveries({ ...filter, status: "error" }, SUMMARY_PAGE, opts);
  return { failed: query.data?.meta.total, isPending: query.isPending };
}
