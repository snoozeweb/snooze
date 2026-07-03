import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { Groups } from "./api";

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

describe("groups.api", () => {
  it("Groups.useList fetches from /api/v1/group", async () => {
    mswServer.use(
      http.get("/api/v1/group", () =>
        HttpResponse.json({
          data: [
            {
              uid: "g1",
              name: "sre",
              description: "SRE team",
              members: [{ username: "alice", method: "local" }],
            },
          ],
          meta: { count: 1, limit: 20, offset: 0, total: 1 },
        }),
      ),
    );
    const { result } = renderHook(() => Groups.useList(), { wrapper: wrap() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.data[0]?.name).toBe("sre");
  });
});
