import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { useFieldSuggestions } from "./useFieldSuggestions";

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

describe("useFieldSuggestions", () => {
  it("unions top-level field names from a sample", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [
            { uid: "1", host: "srv-1", severity: "info", environment: "prod" },
            { uid: "2", host: "srv-2", source: "syslog" },
          ],
          meta: { count: 2, limit: 50, offset: 0, total: 2 },
        }),
      ),
    );
    const { result } = renderHook(() => useFieldSuggestions("record"), { wrapper: wrap() });
    await waitFor(() => expect(result.current.isPending).toBe(false));
    const f = result.current.fields;
    expect(f).toContain("host");
    expect(f).toContain("severity");
    expect(f).toContain("environment");
    expect(f).toContain("source");
    expect(f).toContain("uid");
  });

  it("flattens one level of nested objects with dot notation", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "1", prometheus: { alertname: "x", instance: "h:9100" } }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const { result } = renderHook(() => useFieldSuggestions("record"), { wrapper: wrap() });
    await waitFor(() => expect(result.current.isPending).toBe(false));
    expect(result.current.fields).toContain("prometheus.alertname");
    expect(result.current.fields).toContain("prometheus.instance");
  });

  it("returns isPending=true while loading", () => {
    mswServer.use(
      http.get("/api/v1/record", async () => {
        await new Promise((r) => setTimeout(r, 100));
        return HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 50, offset: 0, total: 0 },
        });
      }),
    );
    const { result } = renderHook(() => useFieldSuggestions("record"), { wrapper: wrap() });
    expect(result.current.isPending).toBe(true);
  });
});
