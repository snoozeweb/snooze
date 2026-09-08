// Deep links from a delivery row into the alerts table.
//
// D12: the links carry the DSL the operator can *see and edit* — the alerts
// route parses `?search=` with the same parseText the SearchBar uses, so the
// query lands in the search box and can be refined from there. Every DSL
// string here is produced by `encodeText`, never hand-formatted, so quoting
// and escaping match the parser exactly.
import { encodeText } from "@/lib/condition/text";
import type { Condition } from "@/lib/condition/types";
import type { DeliveryAlert, DeliveryEntry } from "./types";

/**
 * Cap on how many uids go into a `uid IN [...]` deep link.
 *
 * Budget: ~8000 characters of URL, which is the practical ceiling across
 * servers and proxies (IE's old 2083 limit is long gone, but nginx's default
 * `large_client_header_buffers` line is 8k). One uid costs 36 characters plus
 * `", "` plus percent-encoding of the quotes and spaces — call it 48 encoded
 * characters — so 120 uids is ~5.8 KB of list inside a URL that also carries
 * the origin, the path and the other search params. The earlier 200 came out
 * at ~9.7 KB, i.e. already over budget before the rest of the URL.
 *
 * Past the cap we fall back to the distinct hashes, which are never more
 * numerous and usually far fewer (a batch of the same flapping alert collapses
 * to one hash).
 */
export const MAX_DEEP_LINK_UIDS = 120;

/** Search params understood by the `/web/alerts` route (see app/router.tsx). */
export type AlertsDeepLinkSearch = {
  tab: "all";
  /** Opens the docked inspector on one record. */
  record?: string;
  /** DSL text, rendered verbatim in the SearchBar. */
  search?: string;
};

export type AlertsDeepLink = {
  to: "/web/alerts";
  search: AlertsDeepLinkSearch;
};

function searchLink(cond: Condition): AlertsDeepLink {
  return { to: "/web/alerts", search: { tab: "all", search: encodeText(cond) } };
}

function bareLink(): AlertsDeepLink {
  return { to: "/web/alerts", search: { tab: "all" } };
}

function distinct(values: readonly (string | undefined)[]): string[] {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const v of values) {
    if (!v || seen.has(v)) continue;
    seen.add(v);
    out.push(v);
  }
  return out;
}

/**
 * alertRecordLink opens ONE alert in the alerts inspector.
 *
 * A uid addresses the record directly (`?record=`). Without one — a
 * first-occurrence alert whose record had not landed when the delivery was
 * logged — we fall back to the hash, which the row always carries, as a DSL
 * search. An alert that has since expired lands on an empty list; the row's
 * own snapshot remains the record of what was sent.
 */
export function alertRecordLink(alert: DeliveryAlert): AlertsDeepLink {
  if (alert.uid) return { to: "/web/alerts", search: { tab: "all", record: alert.uid } };
  if (alert.hash) return searchLink({ type: "EQUALS", field: "hash", value: alert.hash });
  return bareLink();
}

/**
 * alertsForDelivery opens every alert covered by one delivery.
 *
 * Prefers `uid IN [...]`, which is exact, but only when the row resolved a uid
 * for *every* member (`alert_uids` may be shorter than `alerts[]`) and the
 * list fits the URL budget. Otherwise it uses the distinct hashes, which are
 * always populated. A single alert degrades to `alertRecordLink` so the
 * inspector opens instead of a one-row filtered list.
 */
export function alertsForDelivery(row: DeliveryEntry): AlertsDeepLink {
  const alerts = row.alerts ?? [];
  const uids = (row.alert_uids ?? []).filter((u): u is string => !!u);
  const hashes = distinct(row.alert_hashes ?? []);

  const single = alerts.length === 1 ? alerts[0] : undefined;
  if (single) return alertRecordLink(single);

  const uidsComplete =
    uids.length > 0 && uids.length === alerts.length && uids.length <= MAX_DEEP_LINK_UIDS;
  if (uidsComplete) return searchLink({ type: "IN", field: "uid", value: uids });

  if (hashes.length === 1) return alertRecordLink({ hash: hashes[0]! });
  if (hashes.length > 1) return searchLink({ type: "IN", field: "hash", value: hashes });

  // No snapshot and no hashes (shouldn't happen — the backend always stores
  // hashes) — use whatever uids the row has rather than dropping the link.
  if (uids.length === 1) return alertRecordLink({ uid: uids[0]! });
  if (uids.length > 1) return searchLink({ type: "IN", field: "uid", value: uids });
  return bareLink();
}
