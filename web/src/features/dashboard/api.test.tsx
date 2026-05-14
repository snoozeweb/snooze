import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { useStats } from "./api";

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
});
