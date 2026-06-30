import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { Heartbeats } from "./api";

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

describe("heartbeats.api", () => {
  it("Heartbeats.useList fetches from /api/v1/heartbeat", async () => {
    mswServer.use(
      http.get("/api/v1/heartbeat", () =>
        HttpResponse.json({
          data: [
            { uid: "hb1", name: "nightly-backup", interval: 86400, status: "ok" },
            { uid: "hb2", name: "hourly-sync", interval: 3600, status: "overdue" },
          ],
          meta: { count: 2, limit: 20, offset: 0, total: 2 },
        }),
      ),
    );
    const { result } = renderHook(() => Heartbeats.useList(), { wrapper: wrap() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.data).toHaveLength(2);
    expect(result.current.data?.data[0]?.name).toBe("nightly-backup");
  });
});
