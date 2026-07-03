import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { TenantMatchRules } from "./api";
import type { TenantMatchRule } from "./types";

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

const SAMPLE: TenantMatchRule = {
  uid: "rule-1",
  match_type: "group",
  match: "ops-team",
  tenant_id: "acme",
  priority: 10,
};

describe("TenantMatchRules.useList", () => {
  it("fetches from /api/v1/tenant_match and returns the data array", async () => {
    mswServer.use(
      http.get("/api/v1/tenant_match", () =>
        HttpResponse.json({
          data: [SAMPLE],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const { result } = renderHook(() => TenantMatchRules.useList(), { wrapper: wrap() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.data).toHaveLength(1);
    expect(result.current.data?.data[0]?.match).toBe("ops-team");
  });
});

describe("TenantMatchRules.useCreate", () => {
  it("POSTs to /api/v1/tenant_match and returns the created rule", async () => {
    mswServer.use(
      http.post("/api/v1/tenant_match", () => HttpResponse.json(SAMPLE, { status: 201 })),
    );
    const { result } = renderHook(() => TenantMatchRules.useCreate(), { wrapper: wrap() });
    const res = await result.current.mutateAsync({
      match_type: "group",
      match: "ops-team",
      tenant_id: "acme",
      priority: 10,
    });
    expect(res.match).toBe("ops-team");
    expect(res.tenant_id).toBe("acme");
  });
});

describe("TenantMatchRules.useUpdate", () => {
  it("PATCHes /api/v1/tenant_match/{uid}", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.patch("/api/v1/tenant_match/rule-1", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ ...SAMPLE, priority: 20 });
      }),
    );
    const { result } = renderHook(() => TenantMatchRules.useUpdate(), { wrapper: wrap() });
    await result.current.mutateAsync({ uid: "rule-1", body: { priority: 20 } });
    expect((bodies[0] as { priority?: number }).priority).toBe(20);
  });
});

describe("TenantMatchRules.useRemove", () => {
  it("DELETEs /api/v1/tenant_match/{uid}", async () => {
    mswServer.use(
      http.delete("/api/v1/tenant_match/rule-1", () => new HttpResponse(null, { status: 204 })),
    );
    const { result } = renderHook(() => TenantMatchRules.useRemove(), { wrapper: wrap() });
    await expect(result.current.mutateAsync("rule-1")).resolves.not.toThrow();
  });
});
