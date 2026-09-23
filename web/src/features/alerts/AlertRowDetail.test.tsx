import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { http, HttpResponse } from "msw";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { mswServer } from "@/tests/msw/server";
import { authStore } from "@/lib/auth/store";
import { decodeConditionQ } from "@/lib/condition/decode";
import type { Condition } from "@/lib/condition/types";
import type { DeliveryEntry } from "@/features/notifications/deliveries/types";
import { AlertRowDetail } from "./AlertRowDetail";
import type { AlertDetailTab } from "./AlertRowDetail";
import type { AgenticEnvelope } from "./analysis/api";
import type { Record_ } from "./types";

function loginWithPerms(perms: string[]) {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(
    JSON.stringify({
      sub: "tester",
      exp: Math.floor(Date.now() / 1000) + 3600,
      permissions: perms,
    }),
  );
  authStore.getState().login(`${header}.${body}.sig`);
}

function leaves(cond: Condition | null): { type: string; field?: string; value?: unknown }[] {
  if (!cond || cond.type !== "AND") return [];
  return cond.args.flatMap((a) =>
    "field" in a && "value" in a
      ? [{ type: a.type, field: a.field, value: a.value as unknown }]
      : [],
  );
}

// Empty comment list so CommentTimeline (the default Timeline tab) resolves to
// its empty state in every test.
function stubComments() {
  mswServer.use(
    http.get("/api/v1/comment", () =>
      HttpResponse.json({ data: [], meta: { count: 0, limit: 100, offset: 0, total: 0 } }),
    ),
  );
  // Every render fires the Deliveries tab's count probe and the header's
  // newest-delivery probe; default to "no deliveries" so tests that don't
  // care about the Deliveries tab aren't left with an unhandled request.
  stubDeliveries([]);
  // Same for the Analysis tab's one GET: 404 is the route's way of saying
  // "this alert carries no analysis", which is the state almost every test
  // here wants.
  stubAnalysis(null);
}

/**
 * Serves GET /api/v1/record/{uid}/agentic. `null` answers 404 — the route's
 * "no analysis yet", not a failure (see analysis/api.ts).
 */
function stubAnalysis(envelope: AgenticEnvelope | null) {
  mswServer.use(
    http.get("/api/v1/record/:uid/agentic", () =>
      envelope === null
        ? HttpResponse.json(
            { error: { code: "not_found", message: "record carries no analysis" } },
            { status: 404 },
          )
        : HttpResponse.json(envelope),
    ),
  );
}

/** A stored analysis for the row the tests render. */
function analysisFor(uid: string, confidence: "high" | "medium" | "low"): AgenticEnvelope {
  return {
    uid,
    agentic: {
      root_cause: {
        summary: "systemd-journald filled /var",
        confidence,
        evidence: ["journalctl: 4.2G under /var/log/journal"],
      },
      remediation_plan: { steps: [{ action: "Vacuum the journal", risk: "low" }] },
      analysis: { at: "2026-09-21T10:00:00Z", by: "agent-bot", source: "alert-rca" },
    },
  };
}

/**
 * Serves GET /api/v1/notificationlog, answering from the `q` CONDITION rather
 * than from the page size. That matters: the inspector's tab-count probe and
 * the timeline's failed-count probe are BOTH `limit=1`, and only the condition
 * tells them apart — a stub keyed on `limit` would hand the "3 failed" number
 * to the "N deliveries" badge and never notice.
 *
 * Returns the recorded requests so a test can assert the scope clause (and
 * that no request was made at all).
 */
function stubDeliveries(rows: DeliveryEntry[]) {
  const seen: { cond: Condition | null; params: URLSearchParams }[] = [];
  mswServer.use(
    http.get("/api/v1/notificationlog", ({ request }) => {
      const url = new URL(request.url);
      const cond = decodeConditionQ(url.searchParams.get("q") ?? "");
      seen.push({ cond, params: url.searchParams });
      const errorOnly = leaves(cond).some((c) => c.field === "status" && c.value === "error");
      const matched = errorOnly ? rows.filter((r) => r.status === "error") : rows;
      const limit = Number(url.searchParams.get("limit") ?? "10");
      const page = matched.slice(0, limit);
      return HttpResponse.json({
        data: page,
        meta: { count: page.length, limit, offset: 0, total: matched.length },
      });
    }),
  );
  return seen;
}

// AlertRowDetail's Flow tab embeds AlertFlowChart, whose entities are TanStack
// <Link>s — so the detail needs a RouterProvider ancestor (app-wide in
// production via app/router.tsx). Stand up a minimal memory router whose home
// route hosts the detail and stubs the deep-link targets so the links resolve.
function renderDetail(
  row: Record_,
  defaultTab?: AlertDetailTab,
  onEditingChange?: (editing: boolean) => void,
) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const root = createRootRoute({ component: () => <Outlet /> });
  const home = createRoute({
    getParentRoute: () => root,
    path: "/",
    component: () => (
      <AlertRowDetail
        row={row}
        {...(defaultTab !== undefined ? { defaultTab } : {})}
        {...(onEditingChange !== undefined ? { onEditingChange } : {})}
      />
    ),
  });
  const stub = (path: string) =>
    createRoute({ getParentRoute: () => root, path, component: () => <div>{path}</div> });
  const tree = root.addChildren([
    home,
    stub("/web/rules"),
    stub("/web/snoozes"),
    stub("/web/notifications"),
  ]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: ["/"] }),
  } as any);
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        {/* The summary header's TimeCell renders a Tooltip, which needs a
            provider ancestor (app-wide in production). */}
        <RouterProvider router={router as Parameters<typeof RouterProvider>[0]["router"]} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

describe("AlertRowDetail", () => {
  beforeEach(() => loginWithPerms(["ro_record", "ro_notificationlog"]));
  afterEach(() => authStore.getState().logout());

  it("renders the summary header (severity, state, source, message, time) without repeating host", () => {
    stubComments();
    const row: Record_ = {
      uid: "r1",
      host: "srv-1",
      severity: "critical",
      state: "open",
      message: "disk full",
      source: "prom",
      date_epoch: 1,
    };
    renderDetail(row);
    // Severity + state badges. Severity renders title-cased ("Critical") for
    // display, with the raw "critical" wire token kept as the badge's title.
    expect(screen.getByText("Critical")).toBeInTheDocument();
    expect(screen.getByText("Open")).toBeInTheDocument();
    // Source chip.
    expect(screen.getByText("prom")).toBeInTheDocument();
    // Message (selectable) is shown directly in the header.
    expect(screen.getByText("disk full")).toBeInTheDocument();
    // Host is the inspector title, so it is NOT repeated in the detail body.
    expect(screen.queryByText("srv-1")).toBeNull();
  });

  it("shows an escalation badge only once the alert has been re-escalated", () => {
    stubComments();
    const base: Record_ = {
      uid: "r1",
      host: "srv-1",
      severity: "critical",
      state: "esc",
      message: "disk full",
      date_epoch: 1,
    };

    // A first-delivery alert carries no extra chrome — just the state chip,
    // which for state=esc already reads "Re-escalated" (the canonical noun
    // shared with the tab/tile/legend).
    const { unmount } = renderDetail(base);
    expect(screen.getAllByText(/Re-escalated/)).toHaveLength(1);
    unmount();

    // Once escalation_count is set, a second badge appears with the count
    // and reason. It deliberately shares the "Re-escalated" headline with
    // the state chip (one word per lifecycle fact) — distinguish by the
    // full "x3 (timeout)" text and by there now being two matches.
    renderDetail({ ...base, escalation_count: 3, escalation_reason: "timeout" });
    expect(screen.getByText("Re-escalated x3 (timeout)")).toBeInTheDocument();
    expect(screen.getAllByText(/Re-escalated/)).toHaveLength(2);
  });

  it("attributes a manual escalation to the operator who made it", () => {
    stubComments();
    renderDetail({
      uid: "r1",
      host: "srv-1",
      severity: "critical",
      state: "esc",
      date_epoch: 1,
      escalation_count: 1,
      escalation_reason: "manual",
      escalation_actor: "alice",
    });
    expect(screen.getByText("Re-escalated by alice")).toBeInTheDocument();
  });

  it("shows Timeline / Flow / Analysis / Deliveries / Record tabs with Timeline active by default", async () => {
    stubComments();
    const row = { uid: "u1", source: "syslog", aggregate: "Host and Message" } as Record_;
    renderDetail(row);
    expect(screen.getByRole("tab", { name: "Timeline" })).toHaveAttribute("data-state", "active");
    expect(screen.getByRole("tab", { name: "Flow" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Record" })).toBeInTheDocument();
    // Deliveries sits right after Flow — both answer "what did the pipeline do
    // with this alert?"; Record is the raw-JSON fallback that closes the strip.
    expect(screen.getAllByRole("tab").map((t) => t.textContent)).toEqual([
      "Timeline",
      "Flow",
      "Analysis",
      "Deliveries",
      "Record",
    ]);
    // Timeline content (its empty state) is visible without interaction.
    await waitFor(() => expect(screen.getByText(/no comments yet/i)).toBeInTheDocument());
  });

  it("reveals the Flow chart and the Record JSON when their tabs are selected", async () => {
    stubComments();
    const row = {
      uid: "u1",
      source: "syslog",
      aggregate: "Host and Message",
      _internal: "secret",
    } as Record_;
    renderDetail(row);
    const user = userEvent.setup();
    // Flow lives behind its tab until selected — the aggregate value is
    // Flow-only (the Aggregate node), unlike source which also sits in the chip.
    expect(screen.queryByText("Host and Message")).toBeNull();
    await user.click(screen.getByRole("tab", { name: "Flow" }));
    expect(screen.getByText("Host and Message")).toBeInTheDocument();
    // Record tab renders the row JSON stripped of underscore-prefixed keys. The
    // uid only appears in this JSON tree (not the summary header), so it's an
    // unambiguous marker that the Record surface is showing.
    await user.click(screen.getByRole("tab", { name: "Record" }));
    expect(screen.getByText(/u1/)).toBeInTheDocument();
    expect(screen.queryByText(/_internal/)).toBeNull();
  });

  it("renders a CommentTimeline scoped to the row's uid", async () => {
    mswServer.use(
      http.get("/api/v1/comment", ({ request }) => {
        const url = new URL(request.url);
        // CommentTimeline filters by record_uid via the resource list query.
        // We just need to return an empty list so the empty state shows.
        // Verify the request is for this record.
        expect(url.searchParams.get("q")).not.toBeNull();
        return HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 100, offset: 0, total: 0 },
        });
      }),
    );
    const row: Record_ = { uid: "r1", host: "srv-1", date_epoch: 1 };
    renderDetail(row);
    await waitFor(() => expect(screen.getByText(/no comments yet/i)).toBeInTheDocument());
  });

  it("labels the Deliveries tab with a count when deliveries exist, and bare otherwise", async () => {
    stubComments();
    stubDeliveries([
      { uid: "d1", date_epoch: 100, status: "success", action: "mail-oncall" },
      { uid: "d2", date_epoch: 90, status: "success", action: "mail-oncall" },
    ]);
    const row: Record_ = { uid: "r1", host: "srv-1", date_epoch: 1 };
    const { unmount } = renderDetail(row);
    await waitFor(() =>
      expect(screen.getByRole("tab", { name: "Deliveries · 2" })).toBeInTheDocument(),
    );
    unmount();

    stubDeliveries([]);
    renderDetail(row);
    await waitFor(() =>
      expect(screen.getByRole("tab", { name: "Deliveries" })).toBeInTheDocument(),
    );
    expect(screen.queryByText(/Deliveries ·/)).toBeNull();
  });

  it("renders the delivery timeline when the Deliveries tab is opened", async () => {
    stubComments();
    stubDeliveries([{ uid: "d1", date_epoch: 100, status: "success", action: "page-oncall" }]);
    const row: Record_ = { uid: "r1", host: "srv-1", date_epoch: 1 };
    renderDetail(row);
    const user = userEvent.setup();
    await waitFor(() =>
      expect(screen.getByRole("tab", { name: "Deliveries · 1" })).toBeInTheDocument(),
    );
    await user.click(screen.getByRole("tab", { name: "Deliveries · 1" }));
    // "page-oncall" also appears in the header's "Last notified … via
    // page-oncall" line, so scope to the action badge inside the timeline row.
    await waitFor(() =>
      expect(screen.getByRole("tabpanel", { name: "Deliveries · 1" })).toHaveTextContent(
        "page-oncall",
      ),
    );
  });

  it("shows a success 'Last notified' header line from the newest delivery", async () => {
    stubComments();
    stubDeliveries([{ uid: "d1", date_epoch: 100, status: "success", action: "mail-oncall" }]);
    const row: Record_ = { uid: "r1", host: "srv-1", date_epoch: 1 };
    renderDetail(row);
    await waitFor(() => expect(screen.getByText(/Last notified/)).toBeInTheDocument());
    expect(screen.getByText("mail-oncall")).toBeInTheDocument();
    expect(screen.queryByText(/Last delivery failed/)).toBeNull();
  });

  it("shows a failed 'Last delivery failed' header line when the newest delivery errored", async () => {
    stubComments();
    stubDeliveries([
      { uid: "d1", date_epoch: 100, status: "error", action: "mail-oncall", error: "dial tcp" },
    ]);
    const row: Record_ = { uid: "r1", host: "srv-1", date_epoch: 1 };
    renderDetail(row);
    await waitFor(() => expect(screen.getByText(/Last delivery failed/)).toBeInTheDocument());
    expect(screen.getByText(/\(mail-oncall\)/)).toBeInTheDocument();
  });

  it("hides the header line entirely when there are no deliveries", async () => {
    stubComments();
    stubDeliveries([]);
    const row: Record_ = { uid: "r1", host: "srv-1", date_epoch: 1 };
    renderDetail(row);
    await waitFor(() =>
      expect(screen.getByRole("tab", { name: "Deliveries" })).toBeInTheDocument(),
    );
    expect(screen.queryByText(/Last notified/)).toBeNull();
    expect(screen.queryByText(/Last delivery failed/)).toBeNull();
  });

  it("scopes the delivery query to the alert uid with a CONTAINS clause", async () => {
    stubComments();
    const seen = stubDeliveries([
      { uid: "d1", date_epoch: 100, status: "success", action: "mail-oncall" },
    ]);
    renderDetail({ uid: "r1", host: "srv-1", date_epoch: 1 });
    await waitFor(() => expect(seen.length).toBeGreaterThan(0));
    expect(leaves(seen[0]!.cond)).toContainEqual({
      type: "CONTAINS",
      field: "alert_uids",
      value: "r1",
    });
  });

  it("counts the tab from the scope probe, not from the failed probe", async () => {
    // Both probes are limit=1; only the condition distinguishes them. If they
    // were confused the tab would read "Deliveries · 1" for 3 deliveries.
    stubComments();
    stubDeliveries([
      { uid: "d1", date_epoch: 100, status: "success", action: "mail-oncall" },
      { uid: "d2", date_epoch: 90, status: "error", action: "mail-oncall", error: "boom" },
      { uid: "d3", date_epoch: 80, status: "success", action: "mail-oncall" },
    ]);
    renderDetail({ uid: "r1", host: "srv-1", date_epoch: 1 });
    const user = userEvent.setup();
    await waitFor(() =>
      expect(screen.getByRole("tab", { name: "Deliveries · 3" })).toBeInTheDocument(),
    );
    await user.click(screen.getByRole("tab", { name: "Deliveries · 3" }));
    // …and the timeline header reads the failed count off its own probe.
    await waitFor(() => expect(screen.getByText(/1 failed/)).toBeInTheDocument());
    expect(screen.getByText("3 deliveries")).toBeInTheDocument();
  });

  it("never queries the log for a row with no uid", async () => {
    // `alert_uids CONTAINS ""` is a regex match on the server: an empty
    // pattern would return every delivery in the tenant under this alert.
    stubComments();
    const seen = stubDeliveries([
      { uid: "d1", date_epoch: 100, status: "success", action: "mail-oncall" },
    ]);
    renderDetail({ host: "srv-1", date_epoch: 1 } as Record_);
    await waitFor(() => expect(screen.getByRole("tab", { name: "Timeline" })).toBeInTheDocument());
    expect(screen.queryByRole("tab", { name: /Deliveries/ })).toBeNull();
    expect(seen).toHaveLength(0);
  });

  it("hides the Deliveries tab — and fires no probe — without ro_notificationlog", async () => {
    authStore.getState().logout();
    loginWithPerms(["ro_record", "ro_notification"]);
    stubComments();
    const seen = stubDeliveries([
      { uid: "d1", date_epoch: 100, status: "success", action: "mail-oncall" },
    ]);
    renderDetail({ uid: "r1", host: "srv-1", date_epoch: 1 });
    await waitFor(() => expect(screen.getByRole("tab", { name: "Timeline" })).toBeInTheDocument());
    expect(screen.queryByRole("tab", { name: /Deliveries/ })).toBeNull();
    expect(screen.queryByText(/Last notified/)).toBeNull();
    expect(seen).toHaveLength(0);
  });

  it("shows the Deliveries tab for a role holding only ro_all", async () => {
    authStore.getState().logout();
    loginWithPerms(["ro_all"]);
    stubComments();
    stubDeliveries([{ uid: "d1", date_epoch: 100, status: "success", action: "mail-oncall" }]);
    renderDetail({ uid: "r1", host: "srv-1", date_epoch: 1 });
    await waitFor(() =>
      expect(screen.getByRole("tab", { name: "Deliveries · 1" })).toBeInTheDocument(),
    );
  });
  it("marks the Analysis tab — with a dot, not a confidence word — once an analysis exists", async () => {
    stubComments();
    stubAnalysis(analysisFor("r1", "medium"));
    const { container } = renderDetail({ uid: "r1", host: "srv-1", date_epoch: 1 });
    await waitFor(() =>
      expect(container.querySelector('[data-slot="analysis-marker"]')).not.toBeNull(),
    );
    // The accessible name stays the plain noun; confidence is said once, in
    // the pane, not on every surface around it.
    expect(screen.getByRole("tab", { name: "Analysis" })).toBeInTheDocument();
    expect(screen.queryByText(/Analysis ·/)).toBeNull();
    expect(screen.queryByText("Medium confidence")).toBeNull();
  });

  it("leaves the Analysis tab bare when the alert carries no analysis", async () => {
    stubComments();
    const { container } = renderDetail({ uid: "r1", host: "srv-1", date_epoch: 1 });
    await waitFor(() => expect(screen.getByRole("tab", { name: "Analysis" })).toBeInTheDocument());
    expect(container.querySelector('[data-slot="analysis-marker"]')).toBeNull();
    expect(screen.queryByRole("button", { name: /AI analysis/ })).toBeNull();
  });

  it("points at the analysis from the header in one line, and opens it on click", async () => {
    const user = userEvent.setup();
    stubComments();
    stubAnalysis(analysisFor("r1", "low"));
    renderDetail({ uid: "r1", host: "srv-1", date_epoch: 1 });
    const line = await screen.findByRole("button", { name: /AI analysis/ });
    expect(line).toHaveTextContent("AI analysis · systemd-journald filled /var");
    // The cause is not printed a second time as a "Cause:" paragraph, and the
    // header carries no confidence chip of its own.
    expect(screen.queryByText("Cause:")).toBeNull();
    expect(screen.queryByText("Low confidence")).toBeNull();

    await user.click(line);
    expect(screen.getByRole("tab", { name: "Analysis" })).toHaveAttribute("data-state", "active");
    expect(screen.getByRole("heading", { name: /What to do/ })).toBeInTheDocument();
    // With the tab open the pane says it all; the pointer would repeat the
    // verdict and the headline a few pixels above themselves.
    expect(screen.queryByRole("button", { name: /AI analysis/ })).toBeNull();
  });

  it("leads the header line with the plan's verdict when one is stated", async () => {
    stubComments();
    const envelope = analysisFor("r1", "high");
    stubAnalysis({
      ...envelope,
      agentic: {
        ...envelope.agentic,
        remediation_plan: { ...envelope.agentic.remediation_plan!, status: "action_required" },
      },
    });
    renderDetail({ uid: "r1", host: "srv-1", date_epoch: 1 });
    const line = await screen.findByRole("button", { name: /AI analysis/ });
    expect(within(line).getByText("Action required")).toBeInTheDocument();
  });

  it("says 'Analysis', not 'AI analysis', for one a person wrote", async () => {
    stubComments();
    const envelope = analysisFor("r1", "high");
    stubAnalysis({
      ...envelope,
      agentic: {
        ...envelope.agentic,
        analysis: { at: "2026-09-21T10:00:00Z", by: "alice", source: "snooze-web" },
      },
    });
    renderDetail({ uid: "r1", host: "srv-1", date_epoch: 1 });
    const line = await screen.findByRole("button", { name: /^Analysis ·/ });
    expect(line).toHaveTextContent("Analysis · systemd-journald filled /var");
    expect(screen.queryByText(/AI analysis/)).toBeNull();
  });

  it("hides the Analysis tab — and fires no probe — for a row with no uid", async () => {
    // The analysis is addressed by its parent record's uid; there is nothing
    // to ask for without one.
    stubComments();
    let hits = 0;
    mswServer.use(
      http.get("/api/v1/record/:uid/agentic", () => {
        hits += 1;
        return HttpResponse.json(analysisFor("r1", "high"));
      }),
    );
    renderDetail({ host: "srv-1", date_epoch: 1 } as Record_);
    await waitFor(() => expect(screen.getByRole("tab", { name: "Timeline" })).toBeInTheDocument());
    expect(screen.queryByRole("tab", { name: /Analysis/ })).toBeNull();
    expect(hits).toBe(0);
  });

  it("opens on the tab the caller asked for", async () => {
    stubComments();
    stubAnalysis(analysisFor("r1", "high"));
    renderDetail({ uid: "r1", host: "srv-1", date_epoch: 1 }, "analysis");
    await waitFor(() =>
      expect(screen.getByRole("tab", { name: "Analysis" })).toHaveAttribute("data-state", "active"),
    );
    // The pane itself is showing, not just the trigger.
    expect(await screen.findByRole("heading", { name: /What to do/ })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Timeline" })).toHaveAttribute("data-state", "inactive");
  });

  it("falls back to Timeline when the tab asked for has no trigger", async () => {
    // ?record=<host+timestamp key>&analysis=1 lands here, as does J/K onto a
    // uid-less row while Analysis is open. The Analysis trigger is hidden for
    // such a row, and a Tabs value with no trigger selects nothing at all —
    // an inspector with five tab stops and no content under any of them.
    stubComments();
    renderDetail({ host: "srv-1", date_epoch: 1 } as Record_, "analysis");
    await waitFor(() =>
      expect(screen.getByRole("tab", { name: "Timeline" })).toHaveAttribute("data-state", "active"),
    );
    // The panel under it is really rendering, not just the trigger.
    expect(screen.getByText(/Open an alert to see its timeline/i)).toBeInTheDocument();
  });

  it("falls back to Timeline when Deliveries is asked for without the permission", async () => {
    stubComments();
    authStore.getState().logout();
    loginWithPerms(["ro_record"]);
    renderDetail({ uid: "r1", host: "srv-1", date_epoch: 1 }, "deliveries");
    await waitFor(() =>
      expect(screen.getByRole("tab", { name: "Timeline" })).toHaveAttribute("data-state", "active"),
    );
    expect(screen.queryByRole("tab", { name: /Deliveries/ })).toBeNull();
  });

  it("leaves the Analysis tab bare when the stored confidence is not one of the three", async () => {
    stubComments();
    const envelope = analysisFor("r1", "high");
    // A level this bundle does not know — from a hand-written document, or a
    // newer server.
    stubAnalysis({
      ...envelope,
      agentic: {
        ...envelope.agentic,
        root_cause: { ...envelope.agentic.root_cause, confidence: "certain" },
      },
    } as unknown as AgenticEnvelope);
    renderDetail({ uid: "r1", host: "srv-1", date_epoch: 1 });
    // The header line still prints — the sentence is readable whatever the
    // confidence says — but nothing around it invents a level.
    expect(await screen.findByRole("button", { name: /AI analysis/ })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Analysis" })).toBeInTheDocument();
    expect(screen.queryByText(/undefined/)).toBeNull();
  });

  it("asks before a tab switch discards an open analysis editor", async () => {
    const user = userEvent.setup();
    authStore.getState().logout();
    loginWithPerms(["ro_record", "rw_protected"]);
    stubComments();
    stubAnalysis(analysisFor("r1", "high"));
    renderDetail({ uid: "r1", host: "srv-1", date_epoch: 1 }, "analysis");

    await user.click(await screen.findByRole("button", { name: "Edit" }));
    expect(await screen.findByLabelText("Summary")).toBeInTheDocument();

    // Radix unmounts the inactive panel, so a stray click on Timeline would
    // silently destroy the draft.
    await user.click(screen.getByRole("tab", { name: "Timeline" }));
    const confirm = await screen.findByRole("dialog", { name: "Discard this analysis draft?" });
    await user.click(within(confirm).getByRole("button", { name: "Keep editing" }));

    expect(screen.getByLabelText("Summary")).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /Analysis/ })).toHaveAttribute("data-state", "active");

    // Asked again and answered, the switch goes through.
    await user.click(screen.getByRole("tab", { name: "Timeline" }));
    await user.click(
      within(await screen.findByRole("dialog", { name: "Discard this analysis draft?" })).getByRole(
        "button",
        { name: "Discard draft" },
      ),
    );
    await waitFor(() =>
      expect(screen.getByRole("tab", { name: "Timeline" })).toHaveAttribute("data-state", "active"),
    );
    expect(screen.queryByLabelText("Summary")).toBeNull();
  });

  it("reports the open editor to its host so a retarget can be guarded", async () => {
    const user = userEvent.setup();
    authStore.getState().logout();
    loginWithPerms(["ro_record", "rw_protected"]);
    stubComments();
    stubAnalysis(analysisFor("r1", "high"));
    const seen: boolean[] = [];
    renderDetail({ uid: "r1", host: "srv-1", date_epoch: 1 }, "analysis", (v) => seen.push(v));

    await user.click(await screen.findByRole("button", { name: "Edit" }));
    await screen.findByLabelText("Summary");
    await waitFor(() => expect(seen.at(-1)).toBe(true));

    await user.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(seen.at(-1)).toBe(false));
  });

  it("shares ONE analysis request between the tab label and the pane", async () => {
    stubComments();
    let hits = 0;
    mswServer.use(
      http.get("/api/v1/record/:uid/agentic", () => {
        hits += 1;
        return HttpResponse.json(analysisFor("r1", "high"));
      }),
    );
    renderDetail({ uid: "r1", host: "srv-1", date_epoch: 1 }, "analysis");
    expect(await screen.findByRole("heading", { name: /What to do/ })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Analysis" })).toBeInTheDocument();
    // Same query key from both readers — TanStack Query dedupes them.
    expect(hits).toBe(1);
  });
});

describe("AlertRowDetail — ownership", () => {
  beforeEach(() => loginWithPerms(["ro_record"]));
  afterEach(() => authStore.getState().logout());

  it("shows the owner beside the state", async () => {
    stubComments();
    mswServer.use(
      http.get("/api/v1/people", () =>
        HttpResponse.json({
          data: [{ name: "alice", method: "local", display_name: "Alice Martin" }],
        }),
      ),
    );
    const { container } = renderDetail({
      uid: "r1",
      state: "ack",
      owner: "alice",
      owner_method: "local",
      owner_since: Math.floor(Date.now() / 1000) - 120,
    });
    await waitFor(() => expect(screen.getByText("Alice Martin")).toBeInTheDocument());
    expect(container.querySelector('[data-slot="owner"]')).toHaveTextContent(
      /Owner Alice Martin · since 2m ago/,
    );
  });

  it("shows the previous owner as a ghost once nobody owns it", async () => {
    stubComments();
    const { container } = renderDetail({
      uid: "r1",
      state: "esc",
      owner: "",
      previous_owner: "bob",
    });
    const line = await waitFor(() => {
      const el = container.querySelector('[data-slot="previous-owner"]');
      expect(el).not.toBeNull();
      return el!;
    });
    expect(line).toHaveTextContent("Unowned · previously bob");
    expect(line.querySelector('[data-variant="ghost"]')).not.toBeNull();
  });
});
