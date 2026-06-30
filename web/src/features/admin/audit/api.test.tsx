import { renderHook, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import type { Condition } from "@/lib/condition/types";
import { useAuthAudit } from "./api";

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function decodeQ(q: string): Condition {
  // Undo the base64url encoding used by encodeConditionQ
  return JSON.parse(atob(q.replace(/-/g, "+").replace(/_/g, "/"))) as Condition;
}

function hasAuthFilter(c: Condition): boolean {
  if (c.type === "EQUALS") return c.field === "object_type" && c.value === "auth";
  if (c.type === "AND" || c.type === "OR") return c.args.some(hasAuthFilter);
  return false;
}

describe("useAuthAudit", () => {
  it("always sends object_type=auth in the encoded q parameter", async () => {
    let capturedUrl: string | undefined;
    mswServer.use(
      http.get("/api/v1/audit", ({ request }) => {
        capturedUrl = request.url;
        return HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 50, offset: 0, total: 0 },
        });
      }),
    );
    renderHook(() => useAuthAudit(undefined, { limit: 50, offset: 0 }), { wrapper });
    await waitFor(() => expect(capturedUrl).toBeDefined());
    const url = new URL(capturedUrl!);
    const q = url.searchParams.get("q");
    expect(q).toBeTruthy();
    const decoded = decodeQ(q!);
    expect(hasAuthFilter(decoded)).toBe(true);
  });

  it("combines an extra caller filter with AND when filter is provided", async () => {
    let capturedUrl: string | undefined;
    mswServer.use(
      http.get("/api/v1/audit", ({ request }) => {
        capturedUrl = request.url;
        return HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 50, offset: 0, total: 0 },
        });
      }),
    );
    const extraFilter: Condition = { type: "EQUALS", field: "username", value: "alice" };
    renderHook(() => useAuthAudit(extraFilter, { limit: 50, offset: 0 }), { wrapper });
    await waitFor(() => expect(capturedUrl).toBeDefined());
    const url = new URL(capturedUrl!);
    const q = url.searchParams.get("q");
    expect(q).toBeTruthy();
    const decoded = decodeQ(q!);
    // The combined condition must still contain object_type=auth
    expect(hasAuthFilter(decoded)).toBe(true);
    // And it must be an AND combining both
    expect(decoded.type).toBe("AND");
    if (decoded.type === "AND") {
      const hasUsernameFilter = decoded.args.some(
        (a) => a.type === "EQUALS" && a.field === "username" && a.value === "alice",
      );
      expect(hasUsernameFilter).toBe(true);
    }
  });
});
