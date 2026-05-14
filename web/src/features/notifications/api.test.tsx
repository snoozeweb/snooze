import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { Actions, Notifications } from "./api";

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

describe("notifications.api", () => {
  it("Notifications.useList fetches from /api/v1/notification", async () => {
    mswServer.use(
      http.get("/api/v1/notification", () =>
        HttpResponse.json({
          data: [{ uid: "n1", name: "Page on-call" }],
          meta: { count: 1, limit: 20, offset: 0, total: 1 },
        }),
      ),
    );
    const { result } = renderHook(() => Notifications.useList(), { wrapper: wrap() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.data[0]?.name).toBe("Page on-call");
  });

  it("Actions.useList fetches from /api/v1/action", async () => {
    mswServer.use(
      http.get("/api/v1/action", () =>
        HttpResponse.json({
          data: [{ uid: "a1", name: "Slack-prod" }],
          meta: { count: 1, limit: 20, offset: 0, total: 1 },
        }),
      ),
    );
    const { result } = renderHook(() => Actions.useList(), { wrapper: wrap() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.data[0]?.name).toBe("Slack-prod");
  });
});
