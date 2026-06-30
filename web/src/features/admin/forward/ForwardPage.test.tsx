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
import { ForwardPage } from "./ForwardPage";

beforeAll(() => {
  if (typeof window !== "undefined" && !window.ResizeObserver) {
    window.ResizeObserver = class ResizeObserver {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
});

const recordHandler = http.get("/api/v1/record", () =>
  HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
);

function setup() {
  const root = createRootRoute({ component: () => <Outlet /> });
  const route = createRoute({
    getParentRoute: () => root,
    path: "/web/admin/forward",
    component: ForwardPage,
  });
  const tree = root.addChildren([route]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: ["/web/admin/forward"] }),
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

describe("ForwardPage", () => {
  it("lists forward destinations", async () => {
    mswServer.use(
      http.get("/api/v1/forward", () =>
        HttpResponse.json({
          data: [
            {
              uid: "f1",
              name: "peer-prod",
              enabled: true,
              endpoint: "https://peer/api/v1/alerts",
              auth: { type: "bearer" },
              event_classes: ["*"],
            },
          ],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
      recordHandler,
    );
    setup();
    await waitFor(() => expect(screen.getByText("peer-prod")).toBeInTheDocument());
    expect(screen.getByText("Bearer token")).toBeInTheDocument();
  });

  it("create button opens the ForwardEditor drawer", async () => {
    mswServer.use(
      http.get("/api/v1/forward", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 50, offset: 0, total: 0 },
        }),
      ),
      recordHandler,
    );
    setup();
    await waitFor(() => expect(screen.getByRole("button", { name: /new/i })).toBeInTheDocument());
    await userEvent.click(screen.getByRole("button", { name: /new/i }));
    await waitFor(() => expect(screen.getByLabelText(/^name$/i)).toBeInTheDocument());
  });
});
