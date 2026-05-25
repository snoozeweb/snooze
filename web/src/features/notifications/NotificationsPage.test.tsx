import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { beforeAll, describe, expect, it } from "vitest";
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
import { NotificationsPage } from "./NotificationsPage";

// Radix UI's BubbleInput (used by Select inside editors) calls ResizeObserver in jsdom.
beforeAll(() => {
  if (typeof window !== "undefined" && !window.ResizeObserver) {
    window.ResizeObserver = class ResizeObserver {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
});

function setup(pathname = "/web/notifications") {
  const root = createRootRoute({ component: () => <Outlet /> });
  const notifications = createRoute({
    getParentRoute: () => root,
    path: "/web/notifications",
    component: NotificationsPage,
  });
  const tree = root.addChildren([notifications]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: [pathname] }),
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

describe("NotificationsPage", () => {
  it("lists notifications in the Notifications tab", async () => {
    mswServer.use(
      http.get("/api/v1/notification", () =>
        HttpResponse.json({
          data: [{ uid: "n1", name: "Page on-call", enabled: true }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
      http.get("/api/v1/action", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 50, offset: 0, total: 0 },
        }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("Page on-call")).toBeInTheDocument());
  });

  it("switches to the Actions tab and lists actions", async () => {
    mswServer.use(
      http.get("/api/v1/notification", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 50, offset: 0, total: 0 },
        }),
      ),
      http.get("/api/v1/action", () =>
        HttpResponse.json({
          data: [{ uid: "a1", name: "Slack-prod", action: { selected: "webhook", subcontent: {} } }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await user.click(screen.getByRole("tab", { name: /actions/i }));
    await waitFor(() => expect(screen.getByText("Slack-prod")).toBeInTheDocument());
  });
});
