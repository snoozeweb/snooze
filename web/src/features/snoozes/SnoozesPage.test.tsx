import { render, screen, waitFor, within } from "@testing-library/react";
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
import { encodeConditionQ } from "@/lib/condition/serialize";
import { SnoozesPage } from "./SnoozesPage";

beforeAll(() => {
  if (typeof window !== "undefined" && !window.ResizeObserver) {
    window.ResizeObserver = class ResizeObserver {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
});

function setup(initialEntry = "/web/snoozes") {
  const root = createRootRoute({ component: () => <Outlet /> });
  const route = createRoute({
    getParentRoute: () => root,
    path: "/web/snoozes",
    component: SnoozesPage,
  });
  const tree = root.addChildren([route]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: [initialEntry] }),
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

describe("SnoozesPage", () => {
  it("lists snoozes", async () => {
    mswServer.use(
      http.get("/api/v1/snooze", () =>
        HttpResponse.json({
          data: [{ uid: "s1", name: "Friday quiet", enabled: true, ttl: 3600 }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("Friday quiet")).toBeInTheDocument());
  });

  it("row-actions kebab includes Copy/Delete, not just Edit and retro-apply", async () => {
    mswServer.use(
      http.get("/api/v1/snooze", () =>
        HttpResponse.json({
          data: [{ uid: "s1", name: "Friday quiet", enabled: true, ttl: 3600 }],
          meta: { count: 1, limit: 1000, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("Friday quiet")).toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: /row actions/i }));
    // Copy/Delete used to only be reachable via the right-click context menu;
    // the visible kebab must offer the same items, not a hand-picked subset.
    expect(screen.getByRole("menuitem", { name: /edit/i })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: /copy as json/i })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: /copy as yaml/i })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: /delete/i })).toBeInTheDocument();
  });

  it("retro-apply on a discard snooze asks for confirmation before hard-deleting matches", async () => {
    let posts = 0;
    mswServer.use(
      http.get("/api/v1/snooze", () =>
        HttpResponse.json({
          data: [{ uid: "s1", name: "Noisy disk", enabled: true, discard: true }],
          meta: { count: 1, limit: 1000, offset: 0, total: 1 },
        }),
      ),
      http.post("/api/v1/snooze/s1/retro_apply", () => {
        posts += 1;
        return HttpResponse.json({ matched: 5, deleted: 5, snooze: "s1" });
      }),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("Noisy disk")).toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: /row actions/i }));
    await user.click(screen.getByRole("menuitem", { name: /retro-apply \(delete matches\)/i }));
    // A discard retro-apply permanently deletes records — it must confirm first.
    const dialog = await screen.findByRole("dialog");
    expect(posts).toBe(0);
    await user.click(within(dialog).getByRole("button", { name: /delete/i }));
    await waitFor(() => expect(posts).toBe(1));
  });

  it("selecting a row switches the toolbar to the amber bulk-actions chip", async () => {
    mswServer.use(
      http.get("/api/v1/snooze", () =>
        HttpResponse.json({
          data: [{ uid: "s1", name: "Friday quiet", enabled: true, ttl: 3600 }],
          meta: { count: 1, limit: 1000, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("Friday quiet")).toBeInTheDocument());
    expect(screen.queryByRole("region", { name: /bulk actions/i })).not.toBeInTheDocument();
    await user.click(screen.getByRole("checkbox", { name: /select row/i }));
    expect(screen.getByRole("region", { name: /bulk actions/i })).toBeInTheDocument();
    expect(screen.getByText("1 selected")).toBeInTheDocument();
  });

  it("lands on the first non-empty tab when Active has none", async () => {
    mswServer.use(
      http.get("/api/v1/snooze", () =>
        HttpResponse.json({
          data: [
            {
              uid: "s1",
              name: "Next quarter freeze",
              enabled: true,
              window_status: "pending",
            },
            { uid: "s2", name: "Last quarter freeze", enabled: true, window_status: "expired" },
          ],
          meta: { count: 2, limit: 1000, offset: 0, total: 2 },
        }),
      ),
    );
    setup();
    // Active (0) would show "No active snoozes" today — the page should
    // redirect to Upcoming (the first non-empty tab) instead.
    await waitFor(() => expect(screen.getByText("Next quarter freeze")).toBeInTheDocument());
    expect(screen.queryByText("Last quarter freeze")).not.toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /upcoming/i })).toHaveAttribute("aria-selected", "true");
  });

  it("stays on an explicitly-requested empty tab (deep link wins)", async () => {
    mswServer.use(
      http.get("/api/v1/snooze", () =>
        HttpResponse.json({
          data: [
            { uid: "s2", name: "Last quarter freeze", enabled: true, window_status: "expired" },
          ],
          meta: { count: 1, limit: 1000, offset: 0, total: 1 },
        }),
      ),
    );
    setup("/web/snoozes?tab=active");
    await waitFor(() => expect(screen.getByText(/no active snoozes/i)).toBeInTheDocument());
    expect(screen.getByRole("tab", { name: /^active/i })).toHaveAttribute("aria-selected", "true");
  });

  it("opens the New snooze editor prefilled from ?prefillCond/prefillName/prefillComment/prefillSeconds", async () => {
    mswServer.use(
      http.get("/api/v1/snooze", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 1000, offset: 0, total: 0 } }),
      ),
    );
    const cond = encodeConditionQ({
      type: "AND",
      args: [
        { type: "EQUALS", field: "host", value: "db01" },
        { type: "EQUALS", field: "message", value: "disk full" },
      ],
    });
    const params: Record<string, string> = {
      prefillCond: cond,
      prefillName: "Snooze — db01",
      prefillComment: "Snoozed from alert r1: disk full",
      prefillSeconds: "3600",
    };
    const qs = Object.entries(params)
      .map(([k, v]) => `${k}=${encodeURIComponent(v)}`)
      .join("&");
    setup(`/web/snoozes?${qs}`);
    // The drawer opens automatically (no click needed) with the name prefilled.
    const nameInput = await screen.findByLabelText(/^name$/i);
    expect(nameInput).toHaveValue("Snooze — db01");
    expect(screen.getByLabelText(/^comment$/i)).toHaveValue("Snoozed from alert r1: disk full");
    // The condition editor renders an input per EQUALS leaf value.
    expect(screen.getByDisplayValue("db01")).toBeInTheDocument();
    expect(screen.getByDisplayValue("disk full")).toBeInTheDocument();
  });
});
