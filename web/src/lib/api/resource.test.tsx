import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { defineResource } from "./resource";

type Rule = { id: string; name: string };

function makeWrapper() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

const Rules = defineResource<Rule>("rule");

describe("defineResource — list", () => {
  it("returns paginated list", async () => {
    mswServer.use(
      http.get("/api/v1/rule", () =>
        HttpResponse.json({ items: [{ id: "r1", name: "Alpha" }], total: 1 }),
      ),
    );
    const wrapper = makeWrapper();
    const { result } = renderHook(() => Rules.useList(), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.items[0]?.name).toBe("Alpha");
    expect(result.current.data?.total).toBe(1);
  });

  it("threads search params into the query string", async () => {
    const seen: string[] = [];
    mswServer.use(
      http.get("/api/v1/rule", ({ request }) => {
        seen.push(new URL(request.url).search);
        return HttpResponse.json({ items: [], total: 0 });
      }),
    );
    const wrapper = makeWrapper();
    const { result } = renderHook(
      () => Rules.useList({ page: 2, pageSize: 50, sortBy: "name", sortOrder: "desc" }),
      { wrapper },
    );
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(seen[0]).toContain("page=2");
    expect(seen[0]).toContain("pageSize=50");
    expect(seen[0]).toContain("sortBy=name");
    expect(seen[0]).toContain("sortOrder=desc");
  });
});

describe("defineResource — get", () => {
  it("returns a single resource by uid", async () => {
    mswServer.use(
      http.get("/api/v1/rule/r1", () => HttpResponse.json({ id: "r1", name: "Alpha" })),
    );
    const wrapper = makeWrapper();
    const { result } = renderHook(() => Rules.useGet("r1"), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.name).toBe("Alpha");
  });

  it("is disabled when uid is undefined", () => {
    const wrapper = makeWrapper();
    const { result } = renderHook(() => Rules.useGet(undefined), { wrapper });
    expect(result.current.fetchStatus).toBe("idle");
  });
});

describe("defineResource — create/update/remove", () => {
  it("create posts to /api/v1/<plugin>", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/rule", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ id: "r2", name: "Created" });
      }),
    );
    const wrapper = makeWrapper();
    const { result } = renderHook(() => Rules.useCreate(), { wrapper });
    await act(async () => {
      await result.current.mutateAsync({ name: "Created" });
    });
    expect(bodies[0]).toEqual({ name: "Created" });
    await waitFor(() => expect(result.current.data?.id).toBe("r2"));
  });

  it("update patches /api/v1/<plugin>/<uid>", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.patch("/api/v1/rule/r1", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ id: "r1", name: "Renamed" });
      }),
    );
    const wrapper = makeWrapper();
    const { result } = renderHook(() => Rules.useUpdate(), { wrapper });
    await act(async () => {
      await result.current.mutateAsync({ uid: "r1", body: { name: "Renamed" } });
    });
    expect(bodies[0]).toEqual({ name: "Renamed" });
    await waitFor(() => expect(result.current.data?.name).toBe("Renamed"));
  });

  it("remove deletes /api/v1/<plugin>/<uid>", async () => {
    const calls: string[] = [];
    mswServer.use(
      http.delete("/api/v1/rule/r1", ({ request }) => {
        calls.push(request.url);
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const wrapper = makeWrapper();
    const { result } = renderHook(() => Rules.useRemove(), { wrapper });
    await act(async () => {
      await result.current.mutateAsync("r1");
    });
    expect(calls[0]).toMatch(/\/api\/v1\/rule\/r1$/);
  });
});

describe("defineResource — query keys", () => {
  it("exposes hierarchical query keys", () => {
    expect(Rules.queryKey.all).toEqual(["rule"]);
    expect(Rules.queryKey.list({ page: 1 })).toEqual(["rule", "list", JSON.stringify({ page: 1 })]);
    expect(Rules.queryKey.one("r1")).toEqual(["rule", "one", "r1"]);
  });
});
