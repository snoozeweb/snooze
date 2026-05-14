import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { Rules, AggregateRules } from "./api";

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

describe("rules.api", () => {
  it("Rules.useList fetches from /api/v1/rule", async () => {
    mswServer.use(
      http.get("/api/v1/rule", () =>
        HttpResponse.json({
          data: [{ uid: "rl1", name: "Tag prod hosts" }],
          meta: { count: 1, limit: 20, offset: 0, total: 1 },
        }),
      ),
    );
    const { result } = renderHook(() => Rules.useList(), { wrapper: wrap() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.data[0]?.name).toBe("Tag prod hosts");
  });

  it("AggregateRules.useList fetches from /api/v1/aggregaterule", async () => {
    mswServer.use(
      http.get("/api/v1/aggregaterule", () =>
        HttpResponse.json({
          data: [{ uid: "ar1", name: "Group by host" }],
          meta: { count: 1, limit: 20, offset: 0, total: 1 },
        }),
      ),
    );
    const { result } = renderHook(() => AggregateRules.useList(), { wrapper: wrap() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.data[0]?.name).toBe("Group by host");
  });
});
