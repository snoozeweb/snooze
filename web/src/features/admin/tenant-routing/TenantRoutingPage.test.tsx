import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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
import { TenantRoutingPage } from "./TenantRoutingPage";

beforeAll(() => {
  if (typeof window !== "undefined" && !window.ResizeObserver) {
    window.ResizeObserver = class ResizeObserver {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
});

const RULES = [
  { uid: "r1", match_type: "group", match: "ops-team", tenant_id: "acme", priority: 0 },
  { uid: "r2", match_type: "domain", match: "example.com", tenant_id: "default", priority: 10 },
];

const TENANTS = {
  data: [
    { id: "acme", display_name: "Acme Corp", status: "active" },
    { id: "default", display_name: "Default", status: "active" },
  ],
  meta: { count: 2, limit: 200, offset: 0, total: 2 },
};

function setup(initialPath = "/web/admin/tenant-routing") {
  const root = createRootRoute({ component: () => <Outlet /> });
  const route = createRoute({
    getParentRoute: () => root,
    path: "/web/admin/tenant-routing",
    component: TenantRoutingPage,
  });
  const tree = root.addChildren([route]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: [initialPath] }),
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

describe("TenantRoutingPage", () => {
  it("lists rules: match_type, match, tenant_id, priority columns are rendered", async () => {
    mswServer.use(
      http.get("/api/v1/tenant_match", () =>
        HttpResponse.json({
          data: RULES,
          meta: { count: 2, limit: 50, offset: 0, total: 2 },
        }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("ops-team")).toBeInTheDocument());
    expect(screen.getByText("example.com")).toBeInTheDocument();
    expect(screen.getByText("acme")).toBeInTheDocument();
    expect(screen.getByText("default")).toBeInTheDocument();
    // Badge labels
    expect(screen.getByText("Group")).toBeInTheDocument();
    expect(screen.getByText("Domain")).toBeInTheDocument();
  });

  it("shows empty state when there are no rules", async () => {
    mswServer.use(
      http.get("/api/v1/tenant_match", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText(/no routing rules yet/i)).toBeInTheDocument());
  });

  it("New rule button opens TenantMatchEditor in create mode", async () => {
    mswServer.use(
      http.get("/api/v1/tenant_match", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
      ),
      http.get("/api/v1/tenant", () => HttpResponse.json(TENANTS)),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() =>
      expect(screen.getByRole("button", { name: /new rule/i })).toBeInTheDocument(),
    );
    await user.click(screen.getByRole("button", { name: /new rule/i }));
    // Editor drawer should open — look for the "Create" submit button
    await waitFor(() =>
      expect(screen.getByRole("button", { name: /create/i })).toBeInTheDocument(),
    );
  });

  it("clicking a row opens TenantMatchEditor in edit mode", async () => {
    mswServer.use(
      http.get("/api/v1/tenant_match", () =>
        HttpResponse.json({
          data: RULES,
          meta: { count: 2, limit: 50, offset: 0, total: 2 },
        }),
      ),
      http.get("/api/v1/tenant_match/r1", () => HttpResponse.json(RULES[0])),
      http.get("/api/v1/tenant", () => HttpResponse.json(TENANTS)),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("ops-team")).toBeInTheDocument());
    await user.click(screen.getByText("ops-team"));
    // Editor should open in edit mode (Save button)
    await waitFor(() => expect(screen.getByRole("button", { name: /save/i })).toBeInTheDocument());
  });

  it("delete via context menu shows confirm dialog, fires DELETE on confirm", async () => {
    const deletedUids: string[] = [];
    mswServer.use(
      http.get("/api/v1/tenant_match", () =>
        HttpResponse.json({
          data: RULES,
          meta: { count: 2, limit: 50, offset: 0, total: 2 },
        }),
      ),
      http.delete("/api/v1/tenant_match/:uid", ({ params }) => {
        deletedUids.push(params["uid"] as string);
        return new HttpResponse(null, { status: 204 });
      }),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("ops-team")).toBeInTheDocument());

    // Right-click on the first data row to open the context menu
    const row = screen.getByText("ops-team").closest("tr")!;
    await user.pointer({ keys: "[MouseRight]", target: row });

    // Click Delete in context menu
    await user.click(await screen.findByRole("menuitem", { name: /^delete$/i }));

    // Confirm dialog appears, then confirm deletion
    await waitFor(() => expect(screen.getByRole("dialog")).toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: /^delete$/i }));

    await waitFor(() => expect(deletedUids).toHaveLength(1));
  });
});
