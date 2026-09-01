import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useMemo, useState } from "react";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import {
  createRootRoute,
  createRouter,
  RouterProvider,
  createMemoryHistory,
} from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { mswServer } from "@/tests/msw/server";
import { usePublishPaletteActions, type PaletteAction } from "@/shared/hooks/usePaletteActions";
import { CommandPalette } from "./CommandPalette";

function Harness({ actions }: { actions?: PaletteAction[] }) {
  const [open, setOpen] = useState(true);
  const list = useMemo(() => actions ?? [], [actions]);
  usePublishPaletteActions(list);
  return <CommandPalette open={open} onOpenChange={setOpen} />;
}

function setup(actions?: PaletteAction[]) {
  const root = createRootRoute({ component: () => <Harness {...(actions ? { actions } : {})} /> });
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-assignment */
  const router = createRouter({
    routeTree: root,
    history: createMemoryHistory({ initialEntries: ["/web/alerts"] }),
  }) as any;
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-assignment */
}

describe("CommandPalette", () => {
  it("renders the search input on open", () => {
    setup();
    expect(screen.getByPlaceholderText(/jump to/i)).toBeInTheDocument();
  });

  it("filters items by query", async () => {
    const user = userEvent.setup();
    setup();
    await user.type(screen.getByPlaceholderText(/jump to/i), "alert");
    expect(screen.getByRole("option", { name: /alerts/i })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: /dashboard/i })).toBeNull();
  });

  it("shows 'No matches' when nothing matches", async () => {
    const user = userEvent.setup();
    setup();
    await user.type(screen.getByPlaceholderText(/jump to/i), "qwertyuiop");
    await waitFor(() => expect(screen.getByText(/no matches/i)).toBeInTheDocument());
  });

  it("surfaces matching alerts under an Alerts group", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-9", message: "disk full on data volume" }],
          meta: { count: 1, limit: 6, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await user.type(screen.getByPlaceholderText(/jump to/i), "disk");
    const hit = await screen.findByRole("option", { name: /disk full on data volume/i });
    expect(hit).toBeInTheDocument();
    expect(hit).toHaveTextContent("srv-9");
  });

  it("keeps navigation entries ahead of alert results", async () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ uid: "r1", host: "srv-9", message: "alert storm" }],
          meta: { count: 1, limit: 6, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await user.type(screen.getByPlaceholderText(/jump to/i), "alert");
    // Wait for the debounced search to land, then check the ordering: a slow
    // query must never move the row Enter is sitting on.
    await screen.findByRole("option", { name: /alert storm/i });
    const options = screen.getAllByRole("option");
    expect(options[0]).toHaveTextContent(/^Alerts/);
  });

  it("lists page-published context actions and runs the selected one", async () => {
    let ran = 0;
    const user = userEvent.setup();
    setup([
      {
        id: "ack-selected",
        label: "Acknowledge selected alerts",
        hint: "3 selected",
        run: () => {
          ran += 1;
        },
      },
    ]);
    await user.type(screen.getByPlaceholderText(/jump to/i), "acknowledge");
    const option = await screen.findByRole("option", { name: /acknowledge selected alerts/i });
    expect(screen.getByText(/on this page/i)).toBeInTheDocument();
    await user.click(option);
    expect(ran).toBe(1);
  });
});
