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
import { RulesPage } from "./RulesPage";

// Radix UI's BubbleInput (used by Select inside RuleEditor) calls ResizeObserver in jsdom.
beforeAll(() => {
  if (typeof window !== "undefined" && !window.ResizeObserver) {
    window.ResizeObserver = class ResizeObserver {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
});

function setup(pathname = "/web/rules") {
  const root = createRootRoute({ component: () => <Outlet /> });
  const rules = createRoute({
    getParentRoute: () => root,
    path: "/web/rules",
    component: RulesPage,
  });
  const tree = root.addChildren([rules]);
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
          {/* router is locally constructed; cast needed for the registered-router type mismatch */}
          <RouterProvider router={router as Parameters<typeof RouterProvider>[0]["router"]} />
          <Toaster />
        </ToastProvider>
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

describe("RulesPage", () => {
  it("lists rules in the Rules tab", async () => {
    mswServer.use(
      http.get("/api/v1/rule", () =>
        HttpResponse.json({
          data: [
            { uid: "rl1", name: "Tag prod", enabled: true, comment: "" },
            { uid: "rl2", name: "Drop noise", enabled: false, comment: "shhh" },
          ],
          meta: { count: 2, limit: 50, offset: 0, total: 2 },
        }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("Tag prod")).toBeInTheDocument());
    expect(screen.getByText("Drop noise")).toBeInTheDocument();
  });

  it("Rules tab: selecting a row switches the toolbar to the amber bulk-actions chip", async () => {
    mswServer.use(
      http.get("/api/v1/rule", () =>
        HttpResponse.json({
          data: [{ uid: "rl1", name: "Tag prod", enabled: true, comment: "" }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("Tag prod")).toBeInTheDocument());
    expect(screen.queryByRole("region", { name: /bulk actions/i })).not.toBeInTheDocument();
    await user.click(screen.getByRole("checkbox", { name: /select rule tag prod/i }));
    expect(screen.getByRole("region", { name: /bulk actions/i })).toBeInTheDocument();
    expect(screen.getByText("1 selected")).toBeInTheDocument();
  });

  it("Rules tab: row kebab View details opens the record/audit drawer", async () => {
    mswServer.use(
      http.get("/api/v1/rule", () =>
        HttpResponse.json({
          data: [{ uid: "rl1", name: "Tag prod", enabled: true, comment: "" }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("Tag prod")).toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: /row actions/i }));
    await user.click(screen.getByRole("menuitem", { name: /view details/i }));
    expect(screen.getByRole("dialog")).toHaveTextContent("Record");
  });

  it("switches to the Aggregates tab and lists aggregate rules", async () => {
    mswServer.use(
      http.get("/api/v1/rule", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 50, offset: 0, total: 0 },
        }),
      ),
      http.get("/api/v1/aggregaterule", () =>
        HttpResponse.json({
          data: [{ uid: "ar1", name: "By host", enabled: true }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await user.click(screen.getByRole("tab", { name: /aggregates/i }));
    await waitFor(() => expect(screen.getByText("By host")).toBeInTheDocument());
  });

  it("Aggregates tab surfaces a discoverable row-actions kebab (not just right-click)", async () => {
    mswServer.use(
      http.get("/api/v1/rule", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
      ),
      http.get("/api/v1/aggregaterule", () =>
        HttpResponse.json({
          data: [{ uid: "ar1", name: "By host", enabled: true }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await user.click(screen.getByRole("tab", { name: /aggregates/i }));
    await waitFor(() => expect(screen.getByText("By host")).toBeInTheDocument());
    expect(screen.getByLabelText("Row actions")).toBeInTheDocument();
  });

  it("Aggregates tab: selecting a row switches the toolbar to the amber bulk-actions chip", async () => {
    mswServer.use(
      http.get("/api/v1/rule", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
      ),
      http.get("/api/v1/aggregaterule", () =>
        HttpResponse.json({
          data: [{ uid: "ar1", name: "By host", enabled: true }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await user.click(screen.getByRole("tab", { name: /aggregates/i }));
    await waitFor(() => expect(screen.getByText("By host")).toBeInTheDocument());
    expect(screen.queryByRole("region", { name: /bulk actions/i })).not.toBeInTheDocument();
    await user.click(screen.getByRole("checkbox", { name: /select row/i }));
    expect(screen.getByRole("region", { name: /bulk actions/i })).toBeInTheDocument();
    expect(screen.getByText("1 selected")).toBeInTheDocument();
  });

  it("switches to the Reject tab and lists reject rules with its helper caption", async () => {
    mswServer.use(
      http.get("/api/v1/rule", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
      ),
      http.get("/api/v1/reject", () =>
        HttpResponse.json({
          data: [{ uid: "rj1", name: "Block legacy", enabled: true }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await user.click(screen.getByRole("tab", { name: /reject/i }));
    await waitFor(() => expect(screen.getByText("Block legacy")).toBeInTheDocument());
    expect(screen.getByText(/return 422 to the sender/i)).toBeInTheDocument();
  });

  it("Reject tab surfaces a discoverable row-actions kebab (not just right-click)", async () => {
    mswServer.use(
      http.get("/api/v1/rule", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
      ),
      http.get("/api/v1/reject", () =>
        HttpResponse.json({
          data: [{ uid: "rj1", name: "Block legacy", enabled: true }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await user.click(screen.getByRole("tab", { name: /reject/i }));
    await waitFor(() => expect(screen.getByText("Block legacy")).toBeInTheDocument());
    expect(screen.getByLabelText("Row actions")).toBeInTheDocument();
  });

  it("Reject tab: selecting a row switches the toolbar to the amber bulk-actions chip", async () => {
    mswServer.use(
      http.get("/api/v1/rule", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
      ),
      http.get("/api/v1/reject", () =>
        HttpResponse.json({
          data: [{ uid: "rj1", name: "Block legacy", enabled: true }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await user.click(screen.getByRole("tab", { name: /reject/i }));
    await waitFor(() => expect(screen.getByText("Block legacy")).toBeInTheDocument());
    expect(screen.queryByRole("region", { name: /bulk actions/i })).not.toBeInTheDocument();
    await user.click(screen.getByRole("checkbox", { name: /select row/i }));
    expect(screen.getByRole("region", { name: /bulk actions/i })).toBeInTheDocument();
    expect(screen.getByText("1 selected")).toBeInTheDocument();
  });

  it("renders all three tab helper captions", () => {
    mswServer.use(
      http.get("/api/v1/rule", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
      ),
    );
    setup();
    expect(screen.getByText(/transform a matching alert, then continue/i)).toBeInTheDocument();
    expect(screen.getByText(/collapse duplicate alerts into one/i)).toBeInTheDocument();
    expect(screen.getByText(/return 422 to the sender/i)).toBeInTheDocument();
  });

  it("explains rule-nesting semantics and links to the rules docs", () => {
    mswServer.use(
      http.get("/api/v1/rule", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
      ),
    );
    setup();
    // Indentation is a conditional gate, not just visual grouping — say so.
    expect(
      screen.getByText(/child rules only run if their parent matched/i),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /learn more/i })).toHaveAttribute(
      "href",
      "https://snoozeweb.github.io/snooze/general/rules",
    );
  });

  it("clicking a reject row opens the RejectEditor", async () => {
    mswServer.use(
      http.get("/api/v1/rule", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
      ),
      http.get("/api/v1/reject", () =>
        HttpResponse.json({
          data: [{ uid: "rj1", name: "Block legacy", enabled: true }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
      http.get("/api/v1/reject/rj1", () =>
        HttpResponse.json({ uid: "rj1", name: "Block legacy", enabled: true }),
      ),
      http.get("/api/v1/record", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await user.click(screen.getByRole("tab", { name: /reject/i }));
    await waitFor(() => expect(screen.getByText("Block legacy")).toBeInTheDocument());
    await user.click(screen.getByText("Block legacy"));
    await waitFor(() =>
      expect(screen.getByRole("dialog", { name: /edit reject rule/i })).toBeInTheDocument(),
    );
  });

  it("clicking a row opens the RuleEditor", async () => {
    mswServer.use(
      http.get("/api/v1/rule", () =>
        HttpResponse.json({
          data: [{ uid: "rl1", name: "Tag prod", enabled: true }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
      http.get("/api/v1/rule/rl1", () =>
        HttpResponse.json({ uid: "rl1", name: "Tag prod", enabled: true }),
      ),
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 50, offset: 0, total: 0 },
        }),
      ),
    );
    const user = userEvent.setup();
    setup();
    await waitFor(() => expect(screen.getByText("Tag prod")).toBeInTheDocument());
    await user.click(screen.getByText("Tag prod"));
    await waitFor(() =>
      expect(screen.getByRole("dialog", { name: /edit rule/i })).toBeInTheDocument(),
    );
  });
});
