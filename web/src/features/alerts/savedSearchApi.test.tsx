import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { SavedSearches } from "./savedSearchApi";

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

describe("savedSearchApi", () => {
  it("SavedSearches.useList fetches from /api/v1/savedsearch", async () => {
    mswServer.use(
      http.get("/api/v1/savedsearch", () =>
        HttpResponse.json({
          data: [
            { uid: "s1", name: "prod criticals", query: "severity = critical", owner: "alice" },
          ],
          meta: { count: 1, limit: 20, offset: 0, total: 1 },
        }),
      ),
    );
    const { result } = renderHook(() => SavedSearches.useList(), { wrapper: wrap() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.data[0]?.name).toBe("prod criticals");
    expect(result.current.data?.data[0]?.query).toBe("severity = critical");
  });

  it("SavedSearches.useCreate POSTs to /api/v1/savedsearch", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/savedsearch", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ uid: "s2", name: "x", query: "host = web" });
      }),
    );
    const { result } = renderHook(() => SavedSearches.useCreate(), { wrapper: wrap() });
    await result.current.mutateAsync({ name: "x", query: "host = web" });
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect((bodies[0] as { name: string }).name).toBe("x");
  });

  it("SavedSearches.useRemove DELETEs /api/v1/savedsearch/{uid}", async () => {
    const deleted: string[] = [];
    mswServer.use(
      http.delete("/api/v1/savedsearch/:uid", ({ params }) => {
        deleted.push(String(params.uid));
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const { result } = renderHook(() => SavedSearches.useRemove(), { wrapper: wrap() });
    await result.current.mutateAsync("s1");
    await waitFor(() => expect(deleted).toEqual(["s1"]));
  });
});
