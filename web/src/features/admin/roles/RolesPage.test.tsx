import { render, screen, waitFor } from "@testing-library/react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
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
import { RolesPage } from "./RolesPage";

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
    path: "/web/admin/roles",
    component: RolesPage,
  });
  const tree = root.addChildren([route]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: ["/web/admin/roles"] }),
  } as any);
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider delay={0}>
        <ToastProvider>
          {/* router is locally constructed; cast needed for the registered-router type mismatch */}
          <RouterProvider router={router as Parameters<typeof RouterProvider>[0]["router"]} />
          <Toaster />
        </ToastProvider>
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

describe("RolesPage", () => {
  it("lists roles", async () => {
    mswServer.use(
      http.get("/api/v1/role", () =>
        HttpResponse.json({
          data: [{ uid: "r1", name: "admin", permissions: ["rw_rule", "ro_rule"] }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("admin")).toBeInTheDocument());
  });

  it("renders ro_tenant with a distinct badge variant (not the same class as ro_record)", async () => {
    mswServer.use(
      http.get("/api/v1/role", () =>
        HttpResponse.json({
          data: [
            {
              uid: "r2",
              name: "platform_admin",
              permissions: ["rw_tenant", "ro_tenant", "ro_record"],
            },
          ],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    setup();
    // Wait for table to load
    await waitFor(() => expect(screen.getByText("platform_admin")).toBeInTheDocument());
    // All three permissions are rendered as badges
    expect(screen.getByText("rw_tenant")).toBeInTheDocument();
    expect(screen.getByText("ro_tenant")).toBeInTheDocument();
    expect(screen.getByText("ro_record")).toBeInTheDocument();
    // rw_tenant and ro_tenant get a different className from ro_record
    // (they use the platform-tier colour, not the standard info colour).
    const rwTenantBadge = screen.getByText("rw_tenant").closest("span");
    const roRecordBadge = screen.getByText("ro_record").closest("span");
    expect(rwTenantBadge?.className).not.toBe(roRecordBadge?.className);
  });
});
