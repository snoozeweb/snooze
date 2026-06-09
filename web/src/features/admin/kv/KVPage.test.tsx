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
import { encodeConditionQ } from "@/lib/condition/serialize";
import { KVPage } from "./KVPage";
import type { KV } from "./types";

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
    path: "/web/admin/kv",
    component: KVPage,
  });
  const tree = root.addChildren([route]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: ["/web/admin/kv"] }),
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

/** Register a `GET /api/v1/kv` handler returning `rows`, recording every
 *  request's query string so tests can assert what the page asked for. */
function mockKV(rows: KV[]): { calls: string[] } {
  const calls: string[] = [];
  mswServer.use(
    http.get("/api/v1/kv", ({ request }) => {
      calls.push(new URL(request.url).search);
      return HttpResponse.json({
        data: rows,
        meta: { count: rows.length, limit: 50, offset: 0, total: rows.length },
      });
    }),
  );
  return { calls };
}

describe("KVPage", () => {
  it("lists key-values", async () => {
    mockKV([{ uid: "k1", dict: "colors", key: "MY_KEY", value: "my-value" }]);
    setup();
    await waitFor(() => expect(screen.getByText("MY_KEY")).toBeInTheDocument());
    expect(screen.getByText("colors")).toBeInTheDocument();
  });

  it("shows an All tab plus one tab per dictionary when several dicts exist", async () => {
    mockKV([
      { uid: "k1", dict: "colors", key: "red", value: "#f00" },
      { uid: "k2", dict: "shapes", key: "circle", value: "1" },
    ]);
    setup();
    await waitFor(() => expect(screen.getByRole("tab", { name: "All" })).toBeInTheDocument());
    expect(screen.getByRole("tab", { name: "colors" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "shapes" })).toBeInTheDocument();
  });

  it("hides the tab bar entirely when only one dictionary is present", async () => {
    mockKV([
      { uid: "k1", dict: "colors", key: "red", value: "#f00" },
      { uid: "k2", dict: "colors", key: "green", value: "#0f0" },
    ]);
    setup();
    await waitFor(() => expect(screen.getByText("red")).toBeInTheDocument());
    expect(screen.queryByRole("tablist")).not.toBeInTheDocument();
  });

  it("filters the list by dictionary when a dict tab is clicked", async () => {
    const { calls } = mockKV([
      { uid: "k1", dict: "colors", key: "red", value: "#f00" },
      { uid: "k2", dict: "shapes", key: "circle", value: "1" },
    ]);
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByRole("tab", { name: "shapes" })).toBeInTheDocument());

    await user.click(screen.getByRole("tab", { name: "shapes" }));

    const expectedQ = encodeConditionQ({ type: "EQUALS", field: "dict", value: "shapes" });
    await waitFor(() => expect(calls.some((s) => s.includes(`q=${expectedQ}`))).toBe(true));
  });
});
