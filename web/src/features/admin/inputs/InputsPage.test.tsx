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
  it("renders the how-to button and catalogue + other-source rows", async () => {
    mswServer.use(
      http.get("/api/v1/inputs", () =>
        HttpResponse.json({
          data: [
            { source: "grafana", last_epoch: 1_700_000_000, count: 3 },
            { source: "graylog", last_epoch: 1_690_000_000, count: 1 },
          ],
        }),
      ),
    );
    setup();
    expect(screen.getByRole("button", { name: /how to send alerts/i })).toBeInTheDocument();
    // The table shows loading skeletons until the activity fetch settles, then
    // renders the (always-present) catalogue rows — REST API is always there —
    // and the uncatalogued "graylog" source lands in "Other sources".
    await waitFor(() => expect(screen.getByText("REST API")).toBeInTheDocument());
    expect(screen.getByText("Other sources")).toBeInTheDocument();
    expect(screen.getByText("graylog")).toBeInTheDocument();
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
});
