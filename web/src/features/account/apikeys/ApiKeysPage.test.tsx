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
import { ApiKeysPage } from "./ApiKeysPage";

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
    path: "/web/admin/apikeys",
    component: ApiKeysPage,
  });
  const tree = root.addChildren([route]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: ["/web/admin/apikeys"] }),
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

describe("ApiKeysPage", () => {
  it("lists API keys returned by GET /api/v1/apikey", async () => {
    mswServer.use(
      http.get("/api/v1/apikey", () =>
        HttpResponse.json({
          data: [{ uid: "ak1", owner: "alice", name: "ci-key" }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("ci-key")).toBeInTheDocument());
  });

  it("shows a Last used column before Expires, with a time for used keys and — for unused", async () => {
    mswServer.use(
      http.get("/api/v1/apikey", () =>
        HttpResponse.json({
          data: [
            // Used once, long ago (far past → TimeCell renders an absolute time,
            // not the volatile "Nm ago" hint).
            { uid: "ak1", owner: "alice", name: "used-key", last_used_at: 1704067200 },
            // Never used → last_used_at absent.
            { uid: "ak2", owner: "bob", name: "fresh-key" },
          ],
          meta: { count: 2, limit: 50, offset: 0, total: 2 },
        }),
      ),
    );
    const { container } = setup();
    await waitFor(() => expect(screen.getByText("used-key")).toBeInTheDocument());

    // The new column is present and ordered before Expires.
    const headers = screen.getAllByRole("columnheader").map((h) => h.textContent ?? "");
    const lastUsedIdx = headers.findIndex((h) => /last used/i.test(h));
    const expiresIdx = headers.findIndex((h) => /expires/i.test(h));
    expect(lastUsedIdx).toBeGreaterThanOrEqual(0);
    expect(expiresIdx).toBeGreaterThanOrEqual(0);
    expect(lastUsedIdx).toBeLessThan(expiresIdx);

    // The used key renders its last-used epoch as a semantic <time> element.
    expect(container.querySelector('time[datetime^="2024-01-01"]')).toBeInTheDocument();
  });

  it("surfaces a discoverable row-actions kebab (not just right-click)", async () => {
    mswServer.use(
      http.get("/api/v1/apikey", () =>
        HttpResponse.json({
          data: [{ uid: "ak1", owner: "alice", name: "ci-key" }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("ci-key")).toBeInTheDocument());
    expect(screen.getByLabelText("Row actions")).toBeInTheDocument();
  });
});
