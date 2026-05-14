import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { StatusPage } from "./StatusPage";

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

describe("StatusPage", () => {
  it("renders cluster members + plugins from the API", async () => {
    mswServer.use(
      http.get("/api/v1/cluster/status", () =>
        HttpResponse.json({
          cluster: {
            members: [
              { name: "snooze1", status: "ok" },
              { name: "snooze2", status: "degraded" },
            ],
            leader: "snooze1",
          },
          plugins: [
            { name: "rule", loaded: true },
            { name: "snooze", loaded: true },
          ],
        }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <StatusPage />
      </Wrapper>,
    );
    await waitFor(() => expect(screen.getByText("snooze1")).toBeInTheDocument());
    expect(screen.getByText("snooze2")).toBeInTheDocument();
    expect(screen.getByText("rule")).toBeInTheDocument();
  });

  it("shows an empty state when the API errors", async () => {
    mswServer.use(
      http.get("/api/v1/cluster/status", () => new HttpResponse(null, { status: 404 })),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <StatusPage />
      </Wrapper>,
    );
    await waitFor(() => expect(screen.getByText(/not available/i)).toBeInTheDocument());
  });
});
