import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import { mswServer } from "@/tests/msw/server";
import { ResourcePage } from "./ResourcePage";

describe("ResourcePage", () => {
  it("renders rule rows fetched from the resource factory", async () => {
    mswServer.use(
      http.get("/api/v1/rule", () =>
        HttpResponse.json({
          data: [
            { id: "r1", name: "alpha", enabled: true, severity: "critical" },
            { id: "r2", name: "beta", enabled: false, severity: "warning" },
          ],
          meta: { count: 2, limit: 10, offset: 0, total: 2 },
        }),
      ),
    );
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={client}>
        <ResourcePage />
      </QueryClientProvider>,
    );
    await waitFor(() => expect(screen.getByText("alpha")).toBeInTheDocument());
    expect(screen.getByText("beta")).toBeInTheDocument();
  });
});
