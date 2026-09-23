// AlertsPage × ownership: the owner filter's URL and query, the retired
// Re-escalated tab, and the Assign / Release flows (row, keyboard, bulk).
// Kept out of AlertsPage.test.tsx, which is already the longest suite here.
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { toastStore } from "@/shared/ui/toast/useToast";
import { mswServer } from "@/tests/msw/server";
import { LiveAnnouncerProvider } from "@/shared/a11y/LiveAnnouncer";
import { decodeConditionQ } from "@/lib/condition/decode";
import { authStore } from "@/lib/auth/store";
import { validateAlertsSearch } from "@/app/alertsSearch";
import { AlertsPage } from "./AlertsPage";

function setup(pathname = "/web/alerts") {
  const root = createRootRoute({ component: () => <Outlet /> });
  const alerts = createRoute({
    getParentRoute: () => root,
    path: "/web/alerts",
    component: AlertsPage,
    validateSearch: validateAlertsSearch,
  });
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: root.addChildren([alerts]),
    history: createMemoryHistory({ initialEntries: [pathname] }),
  } as any);
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <LiveAnnouncerProvider>
          <RouterProvider router={router as Parameters<typeof RouterProvider>[0]["router"]} />
        </LiveAnnouncerProvider>
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return router;
}

function loginAs(sub: string) {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(
    JSON.stringify({
      sub,
      method: "local",
      exp: Math.floor(Date.now() / 1000) + 3600,
      permissions: ["ro_record", "rw_record", "can_comment"],
    }),
  );
  authStore.getState().login(`${header}.${body}.sig`);
}

const PEOPLE = [
  { name: "alice", method: "local", display_name: "Alice Martin" },
  { name: "bob", method: "ldap", display_name: "Bob Stone" },
];

type Row = Record<string, unknown>;

/** Serves `rows` from /record and records every list and owners query. */
function serve(rows: Row[], owners = [{ owner: "bob", count: 2 }], unowned = 1) {
  const listQs: string[] = [];
  const ownerQs: string[] = [];
  mswServer.use(
    http.get("/api/v1/record", ({ request }) => {
      listQs.push(new URL(request.url).searchParams.get("q") ?? "");
      return HttpResponse.json({
        data: rows,
        meta: { count: rows.length, limit: 50, offset: 0, total: rows.length },
      });
    }),
    http.get("/api/v1/record/owners", ({ request }) => {
      ownerQs.push(new URL(request.url).searchParams.get("q") ?? "");
      return HttpResponse.json({ data: owners, unowned, total: 3 });
    }),
    http.get("/api/v1/people", () => HttpResponse.json({ data: PEOPLE })),
  );
  return { listQs, ownerQs };
}

const decoded = (q: string | undefined) => JSON.stringify(q ? decodeConditionQ(q) : null);

describe("AlertsPage — owner filter", () => {
  beforeEach(() => loginAs("alice"));
  afterEach(() => {
    toastStore.clear();
    authStore.getState().logout({ revoke: false });
  });

  it("a chip click pushes ?owner= and ANDs the owner into the list query, not the counts", async () => {
    const { listQs, ownerQs } = serve([{ uid: "r1", host: "srv-1", state: "open", date_epoch: 1 }]);
    const user = userEvent.setup();
    const router = setup();
    const bob = await screen.findByRole("button", { name: "Bob Stone, 2 alerts" });
    const before = router.history.length;
    await user.click(bob);
    await waitFor(() =>
      expect((router.state.location.search as { owner?: string }).owner).toBe("bob"),
    );
    // A history step, like every other filter on this page.
    expect(router.history.length).toBe(before + 1);
    await waitFor(() =>
      expect(decoded(listQs.at(-1))).toContain('{"type":"EQUALS","field":"owner","value":"bob"}'),
    );
    // Counts keep describing the view without the owner filter.
    expect(ownerQs.length).toBeGreaterThan(0);
    for (const q of ownerQs) expect(decoded(q)).not.toContain('"field":"owner"');
    expect(bob).toHaveAttribute("aria-pressed", "true");
  });

  it("reads ?owner=~none from a deep link as Unowned, with a removable chip", async () => {
    const { listQs } = serve([{ uid: "r1", host: "srv-1", state: "open", date_epoch: 1 }]);
    const user = userEvent.setup();
    const router = setup("/web/alerts?owner=~none");
    await waitFor(() => expect(listQs.length).toBeGreaterThan(0));
    const q = decoded(listQs[0]);
    expect(q).toContain('{"type":"NOT","arg":{"type":"EXISTS","field":"owner"}}');
    expect(q).toContain('{"type":"EQUALS","field":"owner","value":""}');
    expect(await screen.findByRole("button", { name: "Unowned, 1 alert" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await user.click(screen.getByRole("button", { name: "Remove Owner filter: Unowned" }));
    await waitFor(() => expect(router.state.location.search).not.toHaveProperty("owner"));
  });

  it("an old ?tab=esc link lands on Alerts and the URL stops saying esc", async () => {
    serve([]);
    const router = setup("/web/alerts?tab=esc");
    await waitFor(() => expect(router.state.location.search).not.toHaveProperty("tab"));
    expect(screen.getByRole("tab", { name: "Alerts" })).toHaveAttribute("aria-selected", "true");
    expect(screen.queryByRole("tab", { name: "Re-escalated" })).toBeNull();
    expect(screen.queryByRole("group", { name: "Active filters" })).toBeNull();
  });
});

describe("AlertsPage — assign and release", () => {
  beforeEach(() => loginAs("alice"));
  afterEach(() => {
    toastStore.clear();
    authStore.getState().logout({ revoke: false });
  });

  function captureComments() {
    const bodies: Array<Record<string, unknown>> = [];
    mswServer.use(
      http.post("/api/v1/comment", async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>);
        return HttpResponse.json({ uid: "c1" });
      }),
    );
    return bodies;
  }

  async function pickPerson(user: ReturnType<typeof userEvent.setup>, name: RegExp) {
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("combobox"));
    await user.click(await screen.findByRole("option", { name }));
    return dialog;
  }

  it("row Assign to… posts an assign comment with the picked person's login and method", async () => {
    serve([{ uid: "r1", host: "srv-1", state: "open", date_epoch: 1 }]);
    const bodies = captureComments();
    const user = userEvent.setup();
    setup();
    await screen.findByText("srv-1");
    await user.click(screen.getAllByRole("button", { name: /row actions/i })[0]!);
    await user.click(screen.getByRole("menuitem", { name: "Assign to…" }));
    const dialog = await pickPerson(user, /Bob Stone \(bob\)/);
    await user.type(within(dialog).getByRole("textbox"), "yours now");
    await user.click(within(dialog).getByRole("button", { name: "Assign" }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(bodies[0]).toEqual({
      record_uid: "r1",
      type: "assign",
      assignee: "bob",
      assignee_method: "ldap",
      message: "yours now",
    });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("refuses to submit Assign without a person, and offers Assign to me", async () => {
    serve([{ uid: "r1", host: "srv-1", state: "open", date_epoch: 1 }]);
    const bodies = captureComments();
    const user = userEvent.setup();
    setup();
    await screen.findByText("srv-1");
    await user.click(screen.getAllByRole("button", { name: /row actions/i })[0]!);
    await user.click(screen.getByRole("menuitem", { name: "Assign to…" }));
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "Assign" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(/choose who/i);
    expect(bodies).toHaveLength(0);
    await user.click(await within(dialog).findByRole("button", { name: "Assign to me" }));
    await user.click(within(dialog).getByRole("button", { name: "Assign" }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(bodies[0]).toMatchObject({
      type: "assign",
      assignee: "alice",
      assignee_method: "local",
    });
  });

  it("offers Release only on owned rows and Assign never on closed ones", async () => {
    serve([
      { uid: "r1", host: "srv-owned", state: "ack", owner: "bob", date_epoch: 2 },
      { uid: "r2", host: "srv-closed", state: "close", date_epoch: 1 },
    ]);
    const bodies = captureComments();
    const user = userEvent.setup();
    setup();
    await screen.findByText("srv-owned");
    const kebabs = screen.getAllByRole("button", { name: /row actions/i });
    await user.click(kebabs[1]!);
    expect(screen.queryByRole("menuitem", { name: "Assign to…" })).toBeNull();
    expect(screen.queryByRole("menuitem", { name: "Release" })).toBeNull();
    await user.keyboard("{Escape}");
    await user.click(kebabs[0]!);
    await user.click(screen.getByRole("menuitem", { name: "Release" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("Release alert")).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Release" }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(bodies[0]).toEqual({ record_uid: "r1", type: "release" });
  });

  it("'o' on the focused row opens the Assign picker", async () => {
    serve([{ uid: "r1", host: "srv-1", state: "open", date_epoch: 1 }]);
    const user = userEvent.setup();
    setup();
    await screen.findByText("srv-1");
    screen.getByRole("grid").focus();
    await user.keyboard("{ArrowDown}o");
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("Assign alert")).toBeInTheDocument();
  });

  it("bulk Assign sends one bulk_owner call over the eligible rows, skipping closed ones", async () => {
    serve([
      { uid: "r1", host: "srv-1", state: "open", date_epoch: 3 },
      { uid: "r2", host: "srv-2", state: "ack", owner: "bob", date_epoch: 2 },
      { uid: "r3", host: "srv-3", state: "close", date_epoch: 1 },
    ]);
    const calls: Array<{ q: string | null; body: unknown }> = [];
    mswServer.use(
      http.post("/api/v1/record/bulk_owner", async ({ request }) => {
        calls.push({
          q: new URL(request.url).searchParams.get("q"),
          body: await request.json(),
        });
        return HttpResponse.json({ matched: 2, updated: 2, action: "assign" });
      }),
    );
    const user = userEvent.setup();
    setup();
    await screen.findByText("srv-1");
    await user.click(screen.getByRole("checkbox", { name: /select all/i }));
    await user.click(screen.getByRole("button", { name: "Assign to… (2 of 3)" }));
    const dialog = await pickPerson(user, /Alice Martin \(alice\) — you/);
    expect(
      within(dialog).getByText(/1 of the 3 selected alerts will be skipped/),
    ).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Assign" }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]!.body).toEqual({
      action: "assign",
      assignee: "alice",
      assignee_method: "local",
    });
    expect(decodeConditionQ(calls[0]!.q ?? "")).toEqual({
      type: "IN",
      field: "uid",
      value: ["r1", "r2"],
    });
    await waitFor(() =>
      expect(
        toastStore
          .getSnapshot()
          .some((t) => /2 alerts assigned to Alice Martin/.test(t.description)),
      ).toBe(true),
    );
  });

  it("bulk Release acts on the owned rows only", async () => {
    serve([
      { uid: "r1", host: "srv-1", state: "open", date_epoch: 2 },
      { uid: "r2", host: "srv-2", state: "ack", owner: "bob", date_epoch: 1 },
    ]);
    const calls: Array<{ q: string | null; body: unknown }> = [];
    mswServer.use(
      http.post("/api/v1/record/bulk_owner", async ({ request }) => {
        calls.push({ q: new URL(request.url).searchParams.get("q"), body: await request.json() });
        return HttpResponse.json({ matched: 1, updated: 1, action: "release" });
      }),
    );
    const user = userEvent.setup();
    setup();
    await screen.findByText("srv-1");
    await user.click(screen.getByRole("checkbox", { name: /select all/i }));
    await user.click(screen.getByRole("button", { name: "Release (1 of 2)" }));
    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "Release" }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]!.body).toEqual({ action: "release" });
    expect(decodeConditionQ(calls[0]!.q ?? "")).toMatchObject({ value: ["r2"] });
  });

  it("an inline ack refreshes the owner counts too (ack takes ownership)", async () => {
    const { ownerQs } = serve([{ uid: "r1", host: "srv-1", state: "open", date_epoch: 1 }]);
    captureComments();
    const user = userEvent.setup();
    setup();
    await screen.findByText("srv-1");
    await waitFor(() => expect(ownerQs.length).toBeGreaterThan(0));
    const before = ownerQs.length;
    await user.click(screen.getAllByRole("button", { name: "Acknowledge" })[0]!);
    await waitFor(() => expect(ownerQs.length).toBeGreaterThan(before));
  });
});
