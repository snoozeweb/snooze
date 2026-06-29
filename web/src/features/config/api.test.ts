import { createElement, type ReactNode } from "react";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import { mswServer } from "@/tests/msw/server";
import { fetchConsoleConfig, useConsoleConfig } from "./api";
import { CONSOLE_FALLBACK } from "./types";

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client }, children);
}

describe("config.api", () => {
  it("useConsoleConfig serves the hardcode fallback as placeholder before the fetch resolves", () => {
    mswServer.use(
      http.get("/api/v1/config", () =>
        HttpResponse.json({
          data: { ...CONSOLE_FALLBACK, refresh_interval: 42, sort_by: "host" },
        }),
      ),
    );
    const { result } = renderHook(() => useConsoleConfig(), { wrapper: wrap() });
    // Synchronously (first render) the query is still pending; placeholderData
    // means callers already read a usable config — the current hardcodes.
    expect(result.current.data?.refresh_interval).toBe(CONSOLE_FALLBACK.refresh_interval);
    expect(result.current.data?.sort_by).toBe(CONSOLE_FALLBACK.sort_by);
  });

  it("useConsoleConfig resolves to the server document", async () => {
    mswServer.use(
      http.get("/api/v1/config", () =>
        HttpResponse.json({
          data: { ...CONSOLE_FALLBACK, refresh_interval: 42, columns: ["severity", "host"] },
        }),
      ),
    );
    const { result } = renderHook(() => useConsoleConfig(), { wrapper: wrap() });
    // placeholderData means isSuccess flips true while still showing the
    // fallback; wait for the real fetch to land (isPlaceholderData === false).
    await waitFor(() => expect(result.current.isPlaceholderData).toBe(false));
    expect(result.current.data?.refresh_interval).toBe(42);
    expect(result.current.data?.columns).toEqual(["severity", "host"]);
  });

  it("fetchConsoleConfig returns the fallback on a server error (non-fatal)", async () => {
    mswServer.use(http.get("/api/v1/config", () => new HttpResponse(null, { status: 500 })));
    const cfg = await fetchConsoleConfig();
    expect(cfg).toEqual(CONSOLE_FALLBACK);
  });

  it("fetchConsoleConfig returns the server document on success", async () => {
    mswServer.use(
      http.get("/api/v1/config", () =>
        HttpResponse.json({ data: { ...CONSOLE_FALLBACK, title: "Acme Ops" } }),
      ),
    );
    const cfg = await fetchConsoleConfig();
    expect(cfg.title).toBe("Acme Ops");
  });
});
