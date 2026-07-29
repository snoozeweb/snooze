import { render, screen, waitFor, within } from "@testing-library/react";
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
import { InputsPage } from "./InputsPage";

beforeAll(() => {
  if (typeof window !== "undefined" && !window.ResizeObserver) {
    window.ResizeObserver = class ResizeObserver {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
});

function setup(initialEntry = "/web/admin/inputs") {
  const root = createRootRoute({ component: () => <Outlet /> });
  const route = createRoute({
    getParentRoute: () => root,
    path: "/web/admin/inputs",
    component: InputsPage,
    validateSearch: (search: Record<string, unknown>) => search as { setup?: string },
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
        {/* router is locally constructed; cast needed for the registered-router type mismatch */}
        <RouterProvider router={router as Parameters<typeof RouterProvider>[0]["router"]} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

describe("InputsPage", () => {
  it("surfaces an error with retry when activity fails to load, without faking an all-idle table", async () => {
    mswServer.use(http.get("/api/v1/inputs", () => new HttpResponse(null, { status: 503 })));
    setup();
    expect(await screen.findByText(/couldn't load ingestion activity/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /retry/i })).toBeInTheDocument();
    // The catalogue is still shown so "how do I set this up" keeps working.
    expect(screen.getByRole("button", { name: /how to receive alerts/i })).toBeInTheDocument();
  });

  it("renders the how-to button and catalogue + other-source rows", async () => {
    mswServer.use(
      http.get("/api/v1/inputs", () =>
        HttpResponse.json({
          data: [
            { source: "grafana", last_epoch: 1_700_000_000, count: 3 },
            { source: "acme-monitor", last_epoch: 1_690_000_000, count: 1 },
          ],
        }),
      ),
    );
    setup();
    expect(screen.getByRole("button", { name: /how to receive alerts/i })).toBeInTheDocument();
    // The table shows loading skeletons until the activity fetch settles, then
    // renders the (always-present) catalogue rows — REST API is always there —
    // and the uncatalogued "acme-monitor" source lands in "Other sources".
    await waitFor(() => expect(screen.getByText("REST API")).toBeInTheDocument());
    expect(screen.getByText("Other sources")).toBeInTheDocument();
    expect(screen.getByText("acme-monitor")).toBeInTheDocument();
  });

  it("opens InjectAlertsDialog pre-focused on a row's Setup button", async () => {
    mswServer.use(http.get("/api/v1/inputs", () => HttpResponse.json({ data: [] })));
    setup();
    await waitFor(() => expect(screen.getByText("Grafana")).toBeInTheDocument());
    const row = screen.getByText("Grafana").closest("tr")!;
    await userEvent.click(within(row).getByRole("button", { name: /setup/i }));
    expect(
      await screen.findByRole("dialog", { name: /how to inject alerts/i }),
    ).toBeInTheDocument();
    expect(screen.getByText("POST /api/v1/webhook/grafana")).toBeInTheDocument();
  });

  it("opens on the REST tab (no pre-selection) from the header button", async () => {
    mswServer.use(http.get("/api/v1/inputs", () => HttpResponse.json({ data: [] })));
    setup();
    // The header CTA calls setSetup("") — the empty-string sentinel is falsy,
    // so setupId resolves to undefined and the dialog opens on the default REST
    // tab with no source pre-selected.
    await userEvent.click(screen.getByRole("button", { name: /how to receive alerts/i }));
    expect(await screen.findByText("How to inject alerts")).toBeInTheDocument();
    // REST panel content proves no webhook/daemon source was pre-selected.
    expect(screen.getByText("POST /api/v1/alerts")).toBeInTheDocument();
  });

  it("opens a 'View details' drawer with the row JSON from the kebab", async () => {
    mswServer.use(
      http.get("/api/v1/inputs", () =>
        HttpResponse.json({
          data: [{ source: "grafana", last_epoch: 1_700_000_000, count: 3 }],
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("Grafana")).toBeInTheDocument());
    const row = screen.getByText("Grafana").closest("tr")!;
    const kebab = within(row).getByRole("button", { name: /row actions/i });
    await user.click(kebab);
    const menuitem = screen.getByRole("menuitem", { name: /view details/i });
    expect(menuitem).toBeInTheDocument();
    await user.click(menuitem);
    const dialog = await screen.findByRole("dialog", { name: /grafana/i });
    expect(within(dialog).getByText(/"webhook"/)).toBeInTheDocument();
  });

  it("opens a 'View details' drawer for an 'other sources' row too", async () => {
    mswServer.use(
      http.get("/api/v1/inputs", () =>
        HttpResponse.json({
          data: [{ source: "acme-monitor", last_epoch: 1_690_000_000, count: 1 }],
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("acme-monitor")).toBeInTheDocument());
    const row = screen.getByText("acme-monitor").closest("tr")!;
    const kebab = within(row).getByRole("button", { name: /row actions/i });
    await user.click(kebab);
    const menuitem = screen.getByRole("menuitem", { name: /view details/i });
    await user.click(menuitem);
    const dialog = await screen.findByRole("dialog", { name: /acme-monitor/i });
    expect(within(dialog).getByText(/"other"/)).toBeInTheDocument();
  });

  it("clears the dialog when closed", async () => {
    mswServer.use(http.get("/api/v1/inputs", () => HttpResponse.json({ data: [] })));
    setup();
    await userEvent.click(screen.getByRole("button", { name: /how to receive alerts/i }));
    expect(await screen.findByText("How to inject alerts")).toBeInTheDocument();
    // Close → onOpenChange(false) → setSetup(undefined) strips ?setup, which
    // unmounts the dialog.
    await userEvent.click(screen.getByRole("button", { name: /^close$/i }));
    await waitFor(() => expect(screen.queryByText("How to inject alerts")).not.toBeInTheDocument());
  });
});
