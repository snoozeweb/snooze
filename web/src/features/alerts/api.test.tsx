import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { Records, useCommentRecord, useShelveRecord } from "./api";

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

describe("useShelveRecord", () => {
  // shelve / unshelve flip the sign on ttl, matching the old Vue
  // toggle_ttl helper. The currentTTL field on the input carries the
  // magnitude so unshelving restores what the user had before.
  async function callShelve(input: { uid: string; shelve: boolean; currentTTL?: number }) {
    const bodies: unknown[] = [];
    mswServer.use(
      http.patch("/api/v1/record/r1", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ uid: "r1" });
      }),
    );
    const wrapper = makeWrapper();
    const { result } = renderHook(() => useShelveRecord(), { wrapper });
    await act(async () => {
      await result.current.mutateAsync(input);
    });
    return bodies;
  }

  it("shelve negates a positive ttl so the magnitude survives unshelving", async () => {
    const bodies = await callShelve({ uid: "r1", shelve: true, currentTTL: 172800 });
    expect(bodies[0]).toEqual({ ttl: -172800 });
  });

  it("shelve falls back to -1 when no current ttl is known", async () => {
    const bodies = await callShelve({ uid: "r1", shelve: true });
    expect(bodies[0]).toEqual({ ttl: -1 });
  });

  it("unshelve restores the magnitude from a negative ttl", async () => {
    const bodies = await callShelve({ uid: "r1", shelve: false, currentTTL: -172800 });
    expect(bodies[0]).toEqual({ ttl: 172800 });
  });

  it("unshelve falls back to the 48h default when ttl is missing", async () => {
    // Pre-stamp legacy rows: shelved but with no magnitude stored. The fallback
    // mirrors the file-config DefaultHousekeeper.RecordTTL.
    const bodies = await callShelve({ uid: "r1", shelve: false });
    expect(bodies[0]).toEqual({ ttl: 48 * 60 * 60 });
  });
});
