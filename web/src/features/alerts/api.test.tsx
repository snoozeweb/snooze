import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { Records, useCommentRecord } from "./api";

function makeWrapper() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

describe("alerts.api", () => {
  it("Records.useList fetches from /api/v1/record", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-1", severity: "critical", state: "open" }],
          meta: { count: 1, limit: 20, offset: 0, total: 1 },
        }),
      ),
    );
    const wrapper = makeWrapper();
    const { result } = renderHook(() => Records.useList({ limit: 20 }), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.data[0]?.host).toBe("srv-1");
  });

  it("useCommentRecord posts to /api/v1/comment with the right body", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/comment", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ ok: true });
      }),
    );
    const wrapper = makeWrapper();
    const { result } = renderHook(() => useCommentRecord(), { wrapper });
    await act(async () => {
      await result.current.mutateAsync({ record_uid: "r1", type: "ack", message: "got it" });
    });
    expect(bodies[0]).toEqual({ record_uid: "r1", type: "ack", message: "got it" });
  });
});
