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
import { AuthAuditPage } from "./AuthAuditPage";

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
    path: "/web/admin/audit",
    component: AuthAuditPage,
  });
  const tree = root.addChildren([route]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: ["/web/admin/audit"] }),
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

describe("AuthAuditPage", () => {
  it("lists auth-audit entries from GET /api/v1/audit", async () => {
    mswServer.use(
      http.get("/api/v1/audit", () =>
        HttpResponse.json({
          data: [
            {
              uid: "e1",
              object_type: "auth",
              object_id: "bob",
              action: "login",
              username: "bob",
              method: "local",
              summary: "login successful",
              date_epoch: 1750000000,
            },
          ],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("bob")).toBeInTheDocument());
    expect(screen.getByText("login")).toBeInTheDocument();
    expect(screen.getByText("local")).toBeInTheDocument();
  });

  it("always queries with object_type=auth regardless of search bar state", async () => {
    let capturedQ: string | undefined;
    mswServer.use(
      http.get("/api/v1/audit", ({ request }) => {
        capturedQ = new URL(request.url).searchParams.get("q") ?? undefined;
        return HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 50, offset: 0, total: 0 },
        });
      }),
    );
    setup();
    await waitFor(() => expect(capturedQ).toBeDefined());
    // Decode and assert object_type=auth is present
    const decoded = JSON.parse(atob(capturedQ!.replace(/-/g, "+").replace(/_/g, "/"))) as Record<
      string,
      unknown
    >;
    const str = JSON.stringify(decoded);
    expect(str).toContain("object_type");
    expect(str).toContain("auth");
  });

  it("shows empty state when no entries are returned", async () => {
    mswServer.use(
      http.get("/api/v1/audit", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 50, offset: 0, total: 0 },
        }),
      ),
    );
    setup();
    await waitFor(() =>
      expect(screen.getByText(/No auth events recorded yet/i)).toBeInTheDocument(),
    );
  });
});
