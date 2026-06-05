import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { Tenants } from "./api";
import type { Tenant } from "./types";

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

const SAMPLE: Tenant = {
  id: "acme",
  display_name: "Acme Corp",
  status: "active",
};

describe("Tenants.useList", () => {
  it("fetches from /api/v1/tenant and returns the data array", async () => {
    mswServer.use(
      http.get("/api/v1/tenant", () =>
        HttpResponse.json({
          data: [SAMPLE],
          meta: { count: 1, limit: 20, offset: 0, total: 1 },
        }),
      ),
    );
    const { result } = renderHook(() => Tenants.useList(), { wrapper: wrap() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.data).toHaveLength(1);
    expect(result.current.data?.data[0]?.id).toBe("acme");
  });
});

describe("Tenants.useCreate", () => {
  it("POSTs to /api/v1/tenant", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/tenant", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ ...SAMPLE }, { status: 201 });
      }),
    );
    const { result } = renderHook(() => Tenants.useCreate(), { wrapper: wrap() });
    await result.current.mutateAsync({ id: "acme", display_name: "Acme Corp", status: "active" });
    expect(bodies).toHaveLength(1);
  });
});

describe("Tenants.useUpdate", () => {
  it("PATCHes /api/v1/tenant/{id}", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.patch("/api/v1/tenant/acme", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ ...SAMPLE, display_name: "Updated" });
      }),
    );
    const { result } = renderHook(() => Tenants.useUpdate(), { wrapper: wrap() });
    await result.current.mutateAsync({ uid: "acme", body: { display_name: "Updated" } });
    expect((bodies[0] as { display_name?: string }).display_name).toBe("Updated");
  });
});

describe("Tenants.useRemove", () => {
  it("DELETEs /api/v1/tenant/{id}", async () => {
    mswServer.use(
      http.delete("/api/v1/tenant/acme", () => new HttpResponse(null, { status: 204 })),
    );
    const { result } = renderHook(() => Tenants.useRemove(), { wrapper: wrap() });
    await expect(result.current.mutateAsync("acme")).resolves.not.toThrow();
  });
});
