import { render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it, beforeAll } from "vitest";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { mswServer } from "@/tests/msw/server";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ToastProvider, Toaster } from "@/shared/ui/Toast";
import { TenantsPage } from "./TenantsPage";

beforeAll(() => {
  if (typeof window !== "undefined" && !window.ResizeObserver) {
    window.ResizeObserver = class ResizeObserver {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
});

function setup() {
  const root = createRootRoute({ component: () => <Outlet /> });
  const route = createRoute({
    getParentRoute: () => root,
    path: "/web/admin/tenants",
    component: TenantsPage,
  });
  const tree = root.addChildren([route]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: ["/web/admin/tenants"] }),
  } as any);
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider delay={0}>
        <ToastProvider>
          <RouterProvider router={router as Parameters<typeof RouterProvider>[0]["router"]} />
          <Toaster />
        </ToastProvider>
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

describe("TenantsPage", () => {
  it("lists tenants returned by GET /api/v1/tenant", async () => {
    mswServer.use(
      http.get("/api/v1/tenant", () =>
        HttpResponse.json({
          data: [
            { id: "default", display_name: "Default", status: "active" },
            { id: "acme", display_name: "Acme Corp", status: "active" },
          ],
          meta: { count: 2, limit: 50, offset: 0, total: 2 },
        }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("acme")).toBeInTheDocument());
    expect(screen.getByText("default")).toBeInTheDocument();
  });

  it("shows the empty state when there are no tenants", async () => {
    mswServer.use(
      http.get("/api/v1/tenant", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText(/no tenants yet/i)).toBeInTheDocument());
  });

  it("renders a New button", async () => {
    setup();
    await waitFor(() => expect(screen.getByRole("button", { name: /new/i })).toBeInTheDocument());
  });
});
