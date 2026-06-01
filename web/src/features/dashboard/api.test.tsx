import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { useStats, useRecentActivity } from "./api";

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

describe("dashboard.api", () => {
  it("useStats fetches /api/v1/stats with from/to/bucket", async () => {
    const seen: URL[] = [];
    mswServer.use(
      http.get("/api/v1/stats", ({ request }) => {
        seen.push(new URL(request.url));
        return HttpResponse.json({
          data: {
            series: [],
            totals: {
              by_severity: { info: 3 },
              by_environment: { prod: 3 },
              by_action_success: {},
              by_action_failure: {},
            },
          },
          meta: { from: "2026-05-13T00:00:00Z", to: "2026-05-14T00:00:00Z", bucket: 3600 },
        });
      }),
    );
    const { result } = renderHook(
      () =>
        useStats({
          from: "2026-05-13T00:00:00Z",
          to: "2026-05-14T00:00:00Z",
          bucket: 3600,
        }),
      { wrapper: wrap() },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.data.totals.by_severity.info).toBe(3);
    expect(seen[0]?.searchParams.get("from")).toBe("2026-05-13T00:00:00Z");
    expect(seen[0]?.searchParams.get("bucket")).toBe("3600");
  });

  it("useRecentActivity fetches recent comments newest-first", async () => {
    mswServer.use(
      http.get("/api/v1/comment", () => {
        return HttpResponse.json({
          data: [
            { uid: "c1", record_uid: "r1", type: "ack", date_epoch: 2000, user: "alice" },
            { uid: "c2", record_uid: "r2", type: "comment", date_epoch: 1000, user: "bob" },
          ],
          meta: { count: 2, limit: 15, offset: 0, total: 2 },
        });
      }),
    );
    const { result } = renderHook(() => useRecentActivity(15), { wrapper: wrap() });
    await waitFor(() => expect(result.current.data?.data).toHaveLength(2));
    expect(result.current.data?.data[0]?.type).toBe("ack");
  });
});
