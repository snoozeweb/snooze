import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { Snoozes } from "./api";

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

describe("snoozes.api", () => {
  it("Snoozes.useList fetches from /api/v1/snooze", async () => {
    mswServer.use(
      http.get("/api/v1/snooze", () =>
        HttpResponse.json({
          data: [{ uid: "s1", name: "Friday quiet", ttl: 3600 }],
          meta: { count: 1, limit: 20, offset: 0, total: 1 },
        }),
      ),
    );
    const { result } = renderHook(() => Snoozes.useList(), { wrapper: wrap() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.data[0]?.name).toBe("Friday quiet");
  });
});
