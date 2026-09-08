import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { decodeConditionQ } from "@/lib/condition/decode";
import type { Condition } from "@/lib/condition/types";
import {
  buildDeliveryCondition,
  deliveryScopeKey,
  useDeliveries,
  useDeliverySummary,
  useFailedDeliveryCount,
} from "./api";

type Seen = { cond: Condition | null; params: URLSearchParams };

function stub(totalFor: (cond: Condition | null) => number, rows: unknown[] = []): Seen[] {
  const seen: Seen[] = [];
  mswServer.use(
    http.get("/api/v1/notificationlog", ({ request }) => {
      const params = new URL(request.url).searchParams;
      const cond = decodeConditionQ(params.get("q") ?? "");
      seen.push({ cond, params });
      return HttpResponse.json({
        data: rows,
        meta: {
          count: rows.length,
          limit: Number(params.get("limit") ?? 0),
          offset: Number(params.get("offset") ?? 0),
          total: totalFor(cond),
        },
      });
    }),
  );
  return seen;
}

function isErrorQuery(cond: Condition | null): boolean {
  return (
    cond?.type === "AND" &&
    cond.args.some((a) => "value" in a && a.field === "status" && a.value === "error")
  );
}

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

describe("buildDeliveryCondition", () => {
  it("matches a notification through the flat uid array", () => {
    expect(buildDeliveryCondition({ kind: "notification", uid: "n1" })).toEqual({
      type: "AND",
      args: [{ type: "CONTAINS", field: "notification_uids", value: "n1" }],
    });
  });

  it("matches an action by name", () => {
    expect(buildDeliveryCondition({ kind: "action", name: "mail-oncall" })).toEqual({
      type: "AND",
      args: [{ type: "EQUALS", field: "action", value: "mail-oncall" }],
    });
  });

  it("matches an alert through the flat uid array", () => {
    expect(buildDeliveryCondition({ kind: "alert", uid: "a1" })).toEqual({
      type: "AND",
      args: [{ type: "CONTAINS", field: "alert_uids", value: "a1" }],
    });
  });

  it("ANDs status, batch and range onto the scope", () => {
    const cond = buildDeliveryCondition({
      kind: "notification",
      uid: "n1",
      status: "error",
      batchOnly: true,
      range: { from: 100, to: 200 },
    });
    expect(cond).toEqual({
      type: "AND",
      args: [
        { type: "CONTAINS", field: "notification_uids", value: "n1" },
        { type: "EQUALS", field: "status", value: "error" },
        // A real boolean: every driver compares booleans by type, so the
        // string "true" would silently match nothing.
        { type: "EQUALS", field: "batch", value: true },
        { type: "GE", field: "date_epoch", value: 100 },
        { type: "LE", field: "date_epoch", value: 200 },
      ],
    });
  });

  it("omits batch when batchOnly is false", () => {
    const cond = buildDeliveryCondition({ kind: "action", name: "a", batchOnly: false });
    expect(cond).toEqual({ type: "AND", args: [{ type: "EQUALS", field: "action", value: "a" }] });
  });

  it("refuses to build a condition for an empty scope", () => {
    // The server evaluates CONTAINS as a regex, so an empty pattern matches
    // EVERY row in the tenant. Throwing is the point: it makes the bug loud
    // instead of returning a query that quietly leaks another alert's history.
    expect(() => buildDeliveryCondition({ kind: "alert", uid: "" })).toThrow(/no identifier/);
    expect(() => buildDeliveryCondition({ kind: "notification", uid: "" })).toThrow(
      /no identifier/,
    );
    expect(() => buildDeliveryCondition({ kind: "action", name: "" })).toThrow(/no identifier/);
  });
});

describe("deliveryScopeKey", () => {
  it("identifies the object, ignoring the narrowing layered on top", () => {
    expect(deliveryScopeKey({ kind: "notification", uid: "n1", status: "error" })).toBe(
      deliveryScopeKey({ kind: "notification", uid: "n1" }),
    );
    expect(deliveryScopeKey({ kind: "alert", uid: "n1" })).not.toBe(
      deliveryScopeKey({ kind: "notification", uid: "n1" }),
    );
  });
});

describe("useDeliveries", () => {
  it("requests the newest rows first", async () => {
    const seen = stub(() => 1);
    const { result } = renderHook(
      () => useDeliveries({ kind: "notification", uid: "n1" }, { limit: 10, offset: 20 }),
      { wrapper: wrap() },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    const p = seen[0]!.params;
    expect(p.get("orderby")).toBe("date_epoch");
    expect(p.get("asc")).toBe("false");
    expect(p.get("limit")).toBe("10");
    expect(p.get("offset")).toBe("20");
  });

  it("never fires for a scope with no identifier", async () => {
    const seen = stub(() => 1);
    const { result } = renderHook(
      () => useDeliveries({ kind: "alert", uid: "" }, { limit: 10, offset: 0 }),
      { wrapper: wrap() },
    );
    await waitFor(() => expect(result.current.fetchStatus).toBe("idle"));
    expect(result.current.isPending).toBe(true);
    expect(seen).toHaveLength(0);
  });

  it("honours enabled:false so a role without the perm sends no 403 probe", async () => {
    const seen = stub(() => 1);
    const { result } = renderHook(
      () =>
        useDeliveries(
          { kind: "notification", uid: "n1" },
          { limit: 10, offset: 0 },
          {
            enabled: false,
          },
        ),
      { wrapper: wrap() },
    );
    await waitFor(() => expect(result.current.fetchStatus).toBe("idle"));
    expect(seen).toHaveLength(0);
  });
});

describe("useDeliverySummary", () => {
  it("answers the count and the newest row from ONE limit=1 request", async () => {
    const seen = stub(() => 142, [{ uid: "d1", date_epoch: 1757340000, action: "mail-oncall" }]);
    const { result } = renderHook(() => useDeliverySummary({ kind: "alert", uid: "a1" }), {
      wrapper: wrap(),
    });
    await waitFor(() => expect(result.current.total).toBe(142));
    expect(result.current.latest?.uid).toBe("d1");
    expect(result.current.isPending).toBe(false);
    // The whole point of W6: the tab badge and the "Last notified" line are
    // one round trip, not two full scans.
    expect(seen).toHaveLength(1);
    const p = seen[0]!.params;
    expect(p.get("limit")).toBe("1");
    expect(p.get("orderby")).toBe("date_epoch");
    expect(p.get("asc")).toBe("false");
  });

  it("stays pending (undefined total) while the request is in flight", () => {
    mswServer.use(http.get("/api/v1/notificationlog", () => new Promise<never>(() => {})));
    const { result } = renderHook(() => useDeliverySummary({ kind: "alert", uid: "a1" }), {
      wrapper: wrap(),
    });
    expect(result.current.total).toBeUndefined();
    expect(result.current.latest).toBeUndefined();
    expect(result.current.isPending).toBe(true);
  });
});

describe("useFailedDeliveryCount", () => {
  it("reads meta.total from a single status=error limit=1 request", async () => {
    const seen = stub((cond) => (isErrorQuery(cond) ? 3 : 142));
    const { result } = renderHook(
      () => useFailedDeliveryCount({ kind: "notification", uid: "n1" }),
      { wrapper: wrap() },
    );
    await waitFor(() => expect(result.current.failed).toBe(3));
    expect(seen).toHaveLength(1);
    expect(isErrorQuery(seen[0]!.cond)).toBe(true);
    expect(seen[0]!.params.get("limit")).toBe("1");
    expect(seen[0]!.params.get("orderby")).toBe("date_epoch");
    expect(seen[0]!.params.get("asc")).toBe("false");
  });
});
