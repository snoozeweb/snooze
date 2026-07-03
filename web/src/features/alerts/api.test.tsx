import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import {
  Records,
  useCommentRecord,
  useShelveRecord,
  useBulkStateRecord,
  useBulkUpdateRecord,
} from "./api";

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

  it("useCommentRecord invalidates BOTH record and comment queries", async () => {
    // A state-changing comment (ack/esc) mutates record state server-side AND
    // appends to the comment log. If we only invalidate one, either the alert
    // list/badge or an open comment timeline goes stale. Post from the timeline
    // composer desynced the list precisely because it invalidated only comments.
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    const spy = vi.spyOn(client, "invalidateQueries");
    mswServer.use(http.post("/api/v1/comment", () => HttpResponse.json({ ok: true })));
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
    const { result } = renderHook(() => useCommentRecord(), { wrapper });
    await act(async () => {
      await result.current.mutateAsync({ record_uid: "r1", type: "ack" });
    });
    const invalidatedKeys = spy.mock.calls.map((c) => JSON.stringify(c[0]?.queryKey));
    expect(invalidatedKeys).toContain(JSON.stringify(["record"]));
    expect(invalidatedKeys).toContain(JSON.stringify(["comment"]));
  });

  it("useCommentRecord posts type=shelve with duration to /api/v1/comment", async () => {
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
      await result.current.mutateAsync({
        record_uid: "r1",
        type: "shelve",
        duration: 14400,
        message: "maintenance",
      });
    });
    expect(bodies[0]).toEqual({
      record_uid: "r1",
      type: "shelve",
      duration: 14400,
      message: "maintenance",
    });
  });

  it("useCommentRecord posts type=unshelve to /api/v1/comment", async () => {
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
      await result.current.mutateAsync({ record_uid: "r1", type: "unshelve" });
    });
    expect(bodies[0]).toEqual({ record_uid: "r1", type: "unshelve" });
  });
});

describe("useBulkStateRecord", () => {
  it("posts to /record/bulk_state with q and state", async () => {
    const calls: Array<{ url: string; body: unknown }> = [];
    mswServer.use(
      http.post("/api/v1/record/bulk_state", async ({ request }) => {
        calls.push({ url: request.url, body: await request.json() });
        return HttpResponse.json({ matched: 3, updated: 3, state: "ack" });
      }),
    );
    const wrapper = makeWrapper();
    const { result } = renderHook(() => useBulkStateRecord(), { wrapper });
    await act(async () => {
      await result.current.mutateAsync({ q: "dGVzdA", state: "ack", message: "maint" });
    });
    expect(calls).toHaveLength(1);
    const url = new URL(calls[0]!.url);
    expect(url.searchParams.get("q")).toBe("dGVzdA");
    expect(calls[0]!.body).toEqual({ state: "ack", message: "maint" });
    expect(result.current.data).toEqual({ matched: 3, updated: 3, state: "ack" });
  });

  it("omits q param when undefined (match-all)", async () => {
    const calls: Array<{ url: string; body: unknown }> = [];
    mswServer.use(
      http.post("/api/v1/record/bulk_state", async ({ request }) => {
        calls.push({ url: request.url, body: await request.json() });
        return HttpResponse.json({ matched: 10, updated: 10, state: "close" });
      }),
    );
    const wrapper = makeWrapper();
    const { result } = renderHook(() => useBulkStateRecord(), { wrapper });
    await act(async () => {
      await result.current.mutateAsync({ state: "close" });
    });
    expect(calls).toHaveLength(1);
    const url = new URL(calls[0]!.url);
    expect(url.searchParams.has("q")).toBe(false);
  });
});

describe("useBulkUpdateRecord", () => {
  it("posts to /record/bulk_update with tag and untag", async () => {
    const calls: Array<{ url: string; body: unknown }> = [];
    mswServer.use(
      http.post("/api/v1/record/bulk_update", async ({ request }) => {
        calls.push({ url: request.url, body: await request.json() });
        return HttpResponse.json({ matched: 2, set: 0, tagged: 2, untagged: 1 });
      }),
    );
    const wrapper = makeWrapper();
    const { result } = renderHook(() => useBulkUpdateRecord(), { wrapper });
    await act(async () => {
      await result.current.mutateAsync({ tag: ["maint"], untag: ["noisy"] });
    });
    expect(calls).toHaveLength(1);
    expect(calls[0]!.body).toEqual({ tag: ["maint"], untag: ["noisy"] });
    expect(result.current.data).toEqual({ matched: 2, set: 0, tagged: 2, untagged: 1 });
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
