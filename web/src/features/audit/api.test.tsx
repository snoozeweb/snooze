import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { useObjectAudit } from "./api";

function wrapper() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

describe("useObjectAudit", () => {
  it("requests newest-first (asc=false) so the recent change lands on page 1", async () => {
    let asc: string | null = null;
    mswServer.use(
      http.get("/api/v1/audit", ({ request }) => {
        asc = new URL(request.url).searchParams.get("asc");
        return HttpResponse.json({ data: [], meta: { count: 0, limit: 5, offset: 0, total: 0 } });
      }),
    );
    const { result } = renderHook(() => useObjectAudit("snooze", "s1"), { wrapper: wrapper() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(asc).toBe("false");
  });
});
