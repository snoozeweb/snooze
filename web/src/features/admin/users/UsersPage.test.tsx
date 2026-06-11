import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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
import { UsersPage } from "./UsersPage";

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
    path: "/web/admin/users",
    component: UsersPage,
  });
  const tree = root.addChildren([route]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: ["/web/admin/users"] }),
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

describe("UsersPage", () => {
  it("lists one user and opens editor on row click", async () => {
    mswServer.use(
      http.get("/api/v1/user", () =>
        HttpResponse.json({
          data: [{ uid: "u1", name: "alice", type: "local" }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
      http.get("/api/v1/user/u1", () =>
        HttpResponse.json({ uid: "u1", name: "alice", type: "local" }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("alice")).toBeInTheDocument());
    // Click the row to open the editor
    await user.click(screen.getByText("alice"));
    await waitFor(() => expect(screen.getByText("Edit user")).toBeInTheDocument());
  });

  it("renders a tab for each redirect (SSO) backend", async () => {
    mswServer.use(
      http.get("/api/v1/login", () =>
        HttpResponse.json({
          data: {
            backends: [
              { name: "local", kind: "password" },
              {
                name: "microsoft",
                kind: "redirect",
                display_name: "Microsoft 365",
                icon: "microsoft",
              },
            ],
            tenants: [],
          },
        }),
      ),
      http.get("/api/v1/user", () =>
        HttpResponse.json({
          data: [{ uid: "u1", name: "alice@egerie.eu", method: "microsoft", enabled: true }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    setup();
    // The SSO backend's display_name becomes a tab.
    await waitFor(() =>
      expect(screen.getByRole("tab", { name: "Microsoft 365" })).toBeInTheDocument(),
    );
    expect(screen.getByRole("tab", { name: "Local" })).toBeInTheDocument();
  });

  it("shows group-derived roles in the Roles column even when roles[] is empty", async () => {
    mswServer.use(
      http.get("/api/v1/role", () =>
        HttpResponse.json({
          data: [{ uid: "r1", name: "admin", groups: ["GrafanaAdmin"] }],
          meta: { count: 1, limit: 500, offset: 0, total: 1 },
        }),
      ),
      http.get("/api/v1/user", () =>
        HttpResponse.json({
          data: [
            {
              uid: "u1",
              name: "florian@egerie.eu",
              method: "microsoft",
              enabled: true,
              roles: [],
              groups: ["GrafanaAdmin", "0165f82e-1673-4d3c-b96a-3c83ce2b057f"],
            },
          ],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("florian@egerie.eu")).toBeInTheDocument());
    // `admin` is granted via the GrafanaAdmin group → it shows despite roles:[].
    await waitFor(() => expect(screen.getByText("admin")).toBeInTheDocument());
  });
});
