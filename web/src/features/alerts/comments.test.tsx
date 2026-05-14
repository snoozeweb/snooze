import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { useRecordComments } from "./comments";

function wrap() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

describe("useRecordComments", () => {
  it("fetches comments for a record", async () => {
    const seen: URL[] = [];
    mswServer.use(
      http.get("/api/v1/comment", ({ request }) => {
        seen.push(new URL(request.url));
        return HttpResponse.json({
          data: [
            {
              uid: "c1",
              record_uid: "r1",
              type: "ack",
              message: "got it",
              date_epoch: 100,
              user: "alice",
            },
            { uid: "c2", record_uid: "r1", type: "close", date_epoch: 200, user: "alice" },
          ],
          meta: { count: 2, limit: 100, offset: 0, total: 2 },
        });
      }),
    );
    const { result } = renderHook(() => useRecordComments("r1"), { wrapper: wrap() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.data).toHaveLength(2);
    expect(seen[0]?.searchParams.get("q")).toBeTruthy();
  });

  it("is disabled when uid is undefined", () => {
    const { result } = renderHook(() => useRecordComments(undefined), { wrapper: wrap() });
    expect(result.current.fetchStatus).toBe("idle");
  });
});
