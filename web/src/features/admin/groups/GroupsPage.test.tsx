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
import { GroupsPage } from "./GroupsPage";

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
    path: "/web/admin/groups",
    component: GroupsPage,
  });
  const tree = root.addChildren([route]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: ["/web/admin/groups"] }),
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

const GROUP_RESPONSE = {
  data: [
    {
      uid: "g1",
      name: "sre",
      description: "SRE team",
      members: [
        { username: "alice", method: "local" },
        { username: "bob", method: "ldap" },
      ],
    },
  ],
  meta: { count: 1, limit: 50, offset: 0, total: 1 },
};

describe("GroupsPage", () => {
  it("lists groups with member count", async () => {
    mswServer.use(http.get("/api/v1/group", () => HttpResponse.json(GROUP_RESPONSE)));
    setup();
    await waitFor(() => expect(screen.getByText("sre")).toBeInTheDocument());
    expect(screen.getByText("2 members")).toBeInTheDocument();
  });

  it("opening a row opens GroupEditor", async () => {
    mswServer.use(
      http.get("/api/v1/group", () => HttpResponse.json(GROUP_RESPONSE)),
      http.get("/api/v1/group/g1", () =>
        HttpResponse.json({
          uid: "g1",
          name: "sre",
          description: "SRE team",
          members: [
            { username: "alice", method: "local" },
            { username: "bob", method: "ldap" },
          ],
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("sre")).toBeInTheDocument());
    await user.click(screen.getByText("sre"));
    await waitFor(() => expect(screen.getByText("Edit group")).toBeInTheDocument());
  });
});
