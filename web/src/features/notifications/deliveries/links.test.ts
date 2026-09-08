import { describe, expect, it } from "vitest";
import { parseText } from "@/lib/condition/text";
import type { Condition } from "@/lib/condition/types";
import { MAX_DEEP_LINK_UIDS, alertRecordLink, alertsForDelivery } from "./links";
import type { DeliveryEntry } from "./types";

// Every deep link's DSL must survive the round trip through the same parser
// the alerts route uses on `?search=` — that is the whole contract of D12.
function roundTrip(text: string | undefined): Condition {
  const res = parseText(text ?? "");
  if (!res.ok) throw new Error(`parse failed: ${res.error.message}`);
  return res.value;
}

function row(over: Partial<DeliveryEntry>): DeliveryEntry {
  return { uid: "d1", date_epoch: 1, action: "mail-oncall", ...over };
}

describe("alertRecordLink", () => {
  it("addresses the record directly when the alert has a uid", () => {
    expect(alertRecordLink({ uid: "a1", hash: "h1" })).toEqual({
      to: "/web/alerts",
      search: { tab: "all", record: "a1" },
    });
  });

  it("falls back to a hash search when the uid was never resolved", () => {
    const link = alertRecordLink({ hash: "h1" });
    expect(link.search.record).toBeUndefined();
    expect(roundTrip(link.search.search)).toEqual({
      type: "EQUALS",
      field: "hash",
      value: "h1",
    });
  });

  it("escapes a hash that is not a bare identifier", () => {
    const link = alertRecordLink({ hash: 'we"ird hash' });
    expect(roundTrip(link.search.search)).toEqual({
      type: "EQUALS",
      field: "hash",
      value: 'we"ird hash',
    });
  });

  it("degrades to the plain alerts tab with neither key", () => {
    expect(alertRecordLink({})).toEqual({ to: "/web/alerts", search: { tab: "all" } });
  });
});

describe("alertsForDelivery", () => {
  it("uses uid IN when every member resolved a uid", () => {
    const link = alertsForDelivery(
      row({
        alert_count: 2,
        alert_uids: ["a1", "a2"],
        alert_hashes: ["h1", "h2"],
        alerts: [
          { uid: "a1", hash: "h1" },
          { uid: "a2", hash: "h2" },
        ],
      }),
    );
    expect(link.to).toBe("/web/alerts");
    expect(link.search.tab).toBe("all");
    expect(roundTrip(link.search.search)).toEqual({
      type: "IN",
      field: "uid",
      value: ["a1", "a2"],
    });
  });

  it("falls back to distinct hashes when a uid is missing", () => {
    const link = alertsForDelivery(
      row({
        alert_count: 3,
        // Only two of the three members resolved a uid.
        alert_uids: ["a1", "a2"],
        alert_hashes: ["h1", "h1", "h2"],
        alerts: [{ hash: "h1" }, { hash: "h1" }, { hash: "h2" }],
      }),
    );
    expect(roundTrip(link.search.search)).toEqual({
      type: "IN",
      field: "hash",
      value: ["h1", "h2"],
    });
  });

  it("still emits uid IN at exactly the cap", () => {
    // The boundary is a URL-length budget, so it has to be inclusive on the
    // last uid that fits — off-by-one here silently downgrades every
    // cap-sized batch to the coarser hash link.
    const uids = Array.from({ length: MAX_DEEP_LINK_UIDS }, (_, i) => `a${i}`);
    const link = alertsForDelivery(
      row({
        alert_count: uids.length,
        alert_uids: uids,
        alert_hashes: uids.map((_, i) => `h${i}`),
        alerts: uids.map((uid, i) => ({ uid, hash: `h${i}` })),
      }),
    );
    expect(roundTrip(link.search.search)).toEqual({ type: "IN", field: "uid", value: uids });
  });

  it("keeps the cap inside the ~8000 character URL budget it documents", () => {
    const uids = Array.from(
      { length: MAX_DEEP_LINK_UIDS },
      () => "01234567-89ab-cdef-0123-456789abcdef",
    );
    const link = alertsForDelivery(
      row({
        alert_count: uids.length,
        alert_uids: uids.map((u, i) => `${u}${i}`),
        alert_hashes: uids.map((_, i) => `h${i}`),
        alerts: uids.map((u, i) => ({ uid: `${u}${i}`, hash: `h${i}` })),
      }),
    );
    const encoded = new URLSearchParams({
      tab: "all",
      search: link.search.search ?? "",
    }).toString();
    expect(encoded.length).toBeLessThan(8000);
  });

  it("falls back to hashes past the uid URL budget", () => {
    const uids = Array.from({ length: MAX_DEEP_LINK_UIDS + 1 }, (_, i) => `a${i}`);
    const link = alertsForDelivery(
      row({
        alert_count: uids.length,
        alert_uids: uids,
        alert_hashes: uids.map(() => "h1"),
        alerts: uids.map((uid) => ({ uid, hash: "h1" })),
      }),
    );
    // Every member shares one hash, so the fallback collapses to a single
    // record link rather than a one-element IN.
    expect(link.search).toEqual({ tab: "all", search: 'hash = "h1"' });
  });

  it("degrades a single-alert delivery to the record link", () => {
    const link = alertsForDelivery(
      row({ alert_count: 1, alert_uids: ["a1"], alert_hashes: ["h1"], alerts: [{ uid: "a1" }] }),
    );
    expect(link.search).toEqual({ tab: "all", record: "a1" });
  });

  it("uses the flat keys when the snapshot is missing entirely", () => {
    const link = alertsForDelivery(row({ alert_count: 2, alert_hashes: ["h1", "h2"] }));
    expect(roundTrip(link.search.search)).toEqual({
      type: "IN",
      field: "hash",
      value: ["h1", "h2"],
    });
  });

  it("degrades to the plain alerts tab with no keys at all", () => {
    expect(alertsForDelivery(row({}))).toEqual({ to: "/web/alerts", search: { tab: "all" } });
  });
});
