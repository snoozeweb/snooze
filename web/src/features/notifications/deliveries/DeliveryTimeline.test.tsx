import { useState } from "react";
import { act, render, screen, waitFor, within } from "@testing-library/react";
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
import { delay, http, HttpResponse } from "msw";
import axe from "axe-core";
import type { AxeResults } from "axe-core";
import { describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { mswServer } from "@/tests/msw/server";
import type { Condition } from "@/lib/condition/types";
import { DELIVERY_REFETCH_MS } from "./api";
import { DeliveryTimeline, type DeliveryTimelineProps } from "./DeliveryTimeline";
import type { DeliveryEntry, DeliveryFilter } from "./types";

function decodeQ(q: string | null): Condition | undefined {
  if (!q) return undefined;
  const b64 = q.replace(/-/g, "+").replace(/_/g, "/");
  return JSON.parse(atob(b64 + "=".repeat((4 - (b64.length % 4)) % 4))) as Condition;
}

function clauses(cond: Condition | undefined): { type: string; field?: string; value?: unknown }[] {
  if (!cond || cond.type !== "AND") return [];
  return cond.args.flatMap((a) =>
    "field" in a && "value" in a
      ? [{ type: a.type, field: a.field, value: a.value as unknown }]
      : [],
  );
}

function scopeOf(cond: Condition | undefined): string {
  const c = clauses(cond).find((x) => x.field === "notification_uids" || x.field === "alert_uids");
  return typeof c?.value === "string" ? c.value : "";
}

/**
 * A log endpoint that actually pages and filters, keyed by scope uid. Needed
 * by the retarget tests: the whole bug class there is about an offset carried
 * across a scope change, which a stub that ignores offset cannot reproduce.
 */
function stubScoped(byScope: Record<string, DeliveryEntry[]>, listDelayMs = 0) {
  const requests: { limit: number; offset: number; cond: Condition | undefined }[] = [];
  mswServer.use(
    http.get("/api/v1/notificationlog", async ({ request }) => {
      const url = new URL(request.url);
      const cond = decodeQ(url.searchParams.get("q"));
      const limit = Number(url.searchParams.get("limit") ?? "10");
      const offset = Number(url.searchParams.get("offset") ?? "0");
      const cl = clauses(cond);
      let rows = byScope[scopeOf(cond)] ?? [];
      if (cl.some((c) => c.field === "status")) rows = rows.filter((r) => r.status === "error");
      if (cl.some((c) => c.field === "batch")) rows = rows.filter((r) => r.batch === true);
      const total = rows.length;
      const page = rows.slice(offset, offset + limit);
      requests.push({ limit, offset, cond });
      // Only the row list is slowed: the count probes (limit=1) must stay
      // instant or the header would be a skeleton for unrelated reasons.
      if (listDelayMs > 0 && limit > 1) await delay(listDelayMs);
      return HttpResponse.json({
        data: page,
        meta: { count: page.length, limit, offset, total },
      });
    }),
  );
  return requests;
}

/**
 * Serves the log endpoint and records every condition it was asked for, so a
 * test can assert what the chips actually sent to the server.
 */
function stubLog(rows: DeliveryEntry[], total = rows.length) {
  const seen: (Condition | undefined)[] = [];
  mswServer.use(
    http.get("/api/v1/notificationlog", ({ request }) => {
      const url = new URL(request.url);
      const cond = decodeQ(url.searchParams.get("q"));
      const limit = Number(url.searchParams.get("limit") ?? "10");
      // limit=1 is a count probe (useDeliveryCounts), not a list render.
      if (limit === 1) {
        const errorOnly = clauses(cond).some((c) => c.field === "status");
        return HttpResponse.json({
          data: [],
          meta: {
            count: 0,
            limit: 1,
            offset: 0,
            total: errorOnly ? rows.filter((r) => r.status === "error").length : total,
          },
        });
      }
      seen.push(cond);
      return HttpResponse.json({
        data: rows,
        meta: { count: rows.length, limit, offset: 0, total },
      });
    }),
  );
  return seen;
}

function renderInRouter(component: () => React.JSX.Element) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const root = createRootRoute({ component: () => <Outlet /> });
  const home = createRoute({
    getParentRoute: () => root,
    path: "/",
    component,
  });
  const alerts = createRoute({
    getParentRoute: () => root,
    path: "/web/alerts",
    component: () => <div>alerts</div>,
    validateSearch: (raw: Record<string, unknown>) => raw,
  });
  const tree = root.addChildren([home, alerts]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: ["/"] }),
  } as any);
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  // The client is handed back so a test can force a refetch (the page-clamp
  // effect only fires on a response that arrives while the page is already
  // out of range — no user gesture produces that).
  return {
    ...render(
      <QueryClientProvider client={client}>
        <TooltipProvider>
          <RouterProvider router={router as Parameters<typeof RouterProvider>[0]["router"]} />
        </TooltipProvider>
      </QueryClientProvider>,
    ),
    client,
  };
}

function renderTimeline(props: Partial<DeliveryTimelineProps> = {}) {
  return renderInRouter(() => (
    <DeliveryTimeline
      filter={{ kind: "notification", uid: "n1" }}
      variant="notification"
      emptyState={<p>No deliveries yet.</p>}
      {...props}
    />
  ));
}

/**
 * The docked drawer RETARGETS the timeline instead of remounting it — prev/next
 * swaps `filter` under a live tree. This mirrors that: one button, one state
 * hook, the same component instance throughout.
 */
function renderRetargetable(first: DeliveryFilter, second: DeliveryFilter) {
  function Harness() {
    const [filter, setFilter] = useState<DeliveryFilter>(first);
    return (
      <>
        <button type="button" onClick={() => setFilter(second)}>
          retarget
        </button>
        <DeliveryTimeline
          filter={filter}
          variant="notification"
          emptyState={<p>No deliveries yet.</p>}
        />
      </>
    );
  }
  return renderInRouter(() => <Harness />);
}

const SENT: DeliveryEntry = {
  uid: "d1",
  date_epoch: 1757340000,
  status: "success",
  action: "mail-oncall",
  notifier: "mail",
  alert_count: 1,
  alert_uids: ["a1"],
  alert_hashes: ["h1"],
  alerts: [{ uid: "a1", hash: "h1", host: "db-01", severity: "critical", message: "disk 98%" }],
};

const FAILED: DeliveryEntry = {
  uid: "d2",
  date_epoch: 1757330000,
  status: "error",
  error: "dial tcp 10.0.0.5:25: connect: connection refused",
  action: "mail-oncall",
  notifier: "mail",
  alert_count: 1,
  alert_uids: ["a2"],
  alert_hashes: ["h2"],
  alerts: [{ uid: "a2", hash: "h2", host: "db-02", severity: "warning", message: "load high" }],
};

const BATCH: DeliveryEntry = {
  uid: "d3",
  date_epoch: 1757320000,
  status: "success",
  action: "webhook-ops",
  notifier: "webhook",
  batch: true,
  batch_reason: "size",
  escalation_count: 2,
  escalation_reason: "timeout",
  alert_count: 7,
  alert_uids: Array.from({ length: 7 }, (_, i) => `b${i}`),
  alert_hashes: Array.from({ length: 7 }, (_, i) => `bh${i}`),
  alerts: Array.from({ length: 7 }, (_, i) => ({
    uid: `b${i}`,
    hash: `bh${i}`,
    host: `web-0${i}`,
    severity: i === 0 ? "critical" : "info",
    message: `member ${i}`,
  })),
};

// Two sends from ONE dispatch: same notification, same alert, same
// `queued_epoch` — the fan-out the log writes as separate rows and the
// timeline folds back into one.
const FANOUT: DeliveryEntry[] = [
  {
    ...SENT,
    uid: "f1",
    queued_epoch: 1757339998,
    notification_uids: ["n1"],
    notification_names: ["notif-a"],
  },
  {
    ...SENT,
    uid: "f2",
    date_epoch: 1757339999,
    queued_epoch: 1757339998,
    status: "error",
    error: "401 unauthorized",
    action: "jira-ops",
    notifier: "jira",
    notification_uids: ["n1"],
    notification_names: ["notif-a"],
  },
];

async function runAxe(node: Element): Promise<AxeResults> {
  return new Promise((resolve, reject) => {
    axe.run(node, (err: Error | null, result: AxeResults) => {
      if (err) reject(err);
      else resolve(result);
    });
  });
}

describe("DeliveryTimeline", () => {
  it("renders the count header and a sent row", async () => {
    stubLog([SENT], 142);
    renderTimeline();
    await waitFor(() => expect(screen.getByText("Sent")).toBeInTheDocument());
    expect(screen.getByText("142 deliveries")).toBeInTheDocument();
    expect(screen.getByText("mail-oncall")).toBeInTheDocument();
    // The notifier rides in the chip's tooltip: the row prints one chip per
    // action, and a column repeating "mail" down every row earns no width.
    expect(screen.getByTitle(/^mail-oncall · mail — Sent/)).toBeInTheDocument();
    // The alert line: severity, host and message, all inside one link.
    const link = screen.getByRole("link", { name: /Open alert: Critical — db-01 — disk 98%/ });
    expect(link).toHaveAttribute("href", expect.stringContaining("record=a1"));
    // Row aria-label summarises the delivery for a screen reader.
    expect(
      screen.getByRole("listitem", { name: /^Sent via mail-oncall, 1 alert, / }),
    ).toBeInTheDocument();
  });

  it("folds one dispatch's actions into a single row, one chip each", async () => {
    stubLog(FANOUT, 2);
    renderTimeline();

    // The header still counts sends — grouping changes the reading, not the
    // log's own arithmetic.
    await waitFor(() => expect(screen.getByText("2 deliveries")).toBeInTheDocument());
    expect(screen.getByText(/1 failed/)).toBeInTheDocument();

    const rows = screen.getAllByRole("listitem", { name: /via/ });
    expect(rows).toHaveLength(1);
    const row = rows[0]!;
    expect(row).toHaveAccessibleName(/^Sent via mail-oncall — failed via jira-ops, 1 alert, /);
    expect(within(row).getByText("mail-oncall")).toBeInTheDocument();
    expect(within(row).getByText("jira-ops")).toBeInTheDocument();
    // One timestamp for the dispatch, not one per action.
    expect(within(row).getAllByRole("time")).toHaveLength(1);
    // The failure names the action it came from, since the row has several.
    expect(within(row).getByText("jira-ops:")).toBeInTheDocument();
    expect(within(row).getByText(/401 unauthorized/)).toBeInTheDocument();
  });

  it("keeps two notifications apart on the alert inspector", async () => {
    stubLog(
      [
        FANOUT[0]!,
        {
          ...FANOUT[0]!,
          uid: "f3",
          notification_uids: ["n2"],
          notification_names: ["notif-b"],
        },
      ],
      2,
    );
    renderTimeline({ variant: "alert", filter: { kind: "alert", uid: "a1" } });

    await waitFor(() => expect(screen.getAllByRole("listitem", { name: /via/ })).toHaveLength(2));
    expect(screen.getByText("notif-a")).toBeInTheDocument();
    expect(screen.getByText("notif-b")).toBeInTheDocument();
  });

  it("omits the failed suffix when nothing failed", async () => {
    stubLog([SENT], 1);
    renderTimeline();
    await waitFor(() => expect(screen.getByText("1 delivery")).toBeInTheDocument());
    expect(screen.queryByText(/failed$/)).not.toBeInTheDocument();
  });

  it("renders a failed row with the error text and a copy button", async () => {
    stubLog([FAILED], 1);
    renderTimeline();
    // "Failed" alone would match the chip toggle, which renders immediately —
    // wait on the error text, which only the row can produce.
    expect(
      await screen.findByText("dial tcp 10.0.0.5:25: connect: connection refused"),
    ).toBeInTheDocument();
    expect(
      within(screen.getByRole("listitem", { name: /^Failed via mail-oncall/ })).getByText("Failed"),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Copy error" })).toBeInTheDocument();
    // Short errors need no expander.
    expect(screen.queryByRole("button", { name: "Show more" })).not.toBeInTheDocument();
  });

  it("expands a long error behind a Show more toggle", async () => {
    stubLog([{ ...FAILED, error: "x".repeat(400) }], 1);
    renderTimeline();
    const toggle = await screen.findByRole("button", { name: "Show more" });
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    await userEvent.click(toggle);
    expect(screen.getByRole("button", { name: "Show less" })).toHaveAttribute(
      "aria-expanded",
      "true",
    );
  });

  it("shows the batch and escalation badges plus a View all link", async () => {
    stubLog([BATCH], 1);
    renderTimeline();
    await waitFor(() => expect(screen.getByText("Batch · 7 alerts")).toBeInTheDocument());
    expect(screen.getByText("Re-escalated x2 (timeout)")).toBeInTheDocument();
    const viewAll = screen.getByRole("link", { name: "View all 7 alerts →" });
    expect(viewAll).toHaveAttribute("href", expect.stringContaining("uid%20IN"));
  });

  it("shows the first five alerts and expands the rest inline", async () => {
    stubLog([BATCH], 1);
    renderTimeline();
    await waitFor(() => expect(screen.getByText("member 0")).toBeInTheDocument());
    expect(screen.queryByText("member 6")).not.toBeInTheDocument();
    const more = screen.getByRole("button", { name: "+2 more" });
    await userEvent.click(more);
    expect(screen.getByText("member 6")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Show fewer" })).toBeInTheDocument();
  });

  it("suppresses alert lines in the alert variant", async () => {
    stubLog([SENT], 1);
    renderTimeline({ variant: "alert", filter: { kind: "alert", uid: "a1" } });
    await waitFor(() => expect(screen.getByText("Sent")).toBeInTheDocument());
    expect(screen.queryByText("disk 98%")).not.toBeInTheDocument();
  });

  it("adds status / batch clauses to the request when a chip is selected", async () => {
    const seen = stubLog([SENT], 3);
    renderTimeline();
    await waitFor(() => expect(seen.length).toBeGreaterThan(0));

    await userEvent.click(screen.getByRole("button", { name: "Failed" }));
    await waitFor(() =>
      expect(clauses(seen[seen.length - 1])).toContainEqual({
        type: "EQUALS",
        field: "status",
        value: "error",
      }),
    );
    expect(screen.getByRole("button", { name: "Failed" })).toHaveAttribute("aria-pressed", "true");

    await userEvent.click(screen.getByRole("button", { name: "Batched" }));
    await waitFor(() =>
      expect(clauses(seen[seen.length - 1])).toContainEqual({
        type: "EQUALS",
        field: "batch",
        value: true,
      }),
    );
    // Chips are exclusive — the status clause is gone again.
    expect(clauses(seen[seen.length - 1])).not.toContainEqual({
      type: "EQUALS",
      field: "status",
      value: "error",
    });
  });

  it("shows the chip-specific empty text instead of the parent empty state", async () => {
    const rows: DeliveryEntry[] = [SENT];
    mswServer.use(
      http.get("/api/v1/notificationlog", ({ request }) => {
        const url = new URL(request.url);
        const cond = decodeQ(url.searchParams.get("q"));
        const filtered = clauses(cond).some((c) => c.field === "status") ? [] : rows;
        return HttpResponse.json({
          data: Number(url.searchParams.get("limit")) === 1 ? [] : filtered,
          meta: { count: filtered.length, limit: 10, offset: 0, total: filtered.length },
        });
      }),
    );
    renderTimeline();
    await waitFor(() => expect(screen.getByText("Sent")).toBeInTheDocument());
    await userEvent.click(screen.getByRole("button", { name: "Failed" }));
    await waitFor(() => expect(screen.getByText("No failed deliveries.")).toBeInTheDocument());
    expect(screen.queryByText("No deliveries yet.")).not.toBeInTheDocument();
  });

  it("renders the parent empty state when the scope has no deliveries", async () => {
    stubLog([], 0);
    renderTimeline();
    await waitFor(() => expect(screen.getByText("No deliveries yet.")).toBeInTheDocument());
  });

  it("renders an inline error with a retry button", async () => {
    mswServer.use(
      http.get("/api/v1/notificationlog", () =>
        HttpResponse.json({ error: { code: "internal", message: "boom" } }, { status: 500 }),
      ),
    );
    renderTimeline();
    await waitFor(() => expect(screen.getByRole("alert")).toBeInTheDocument());
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });

  it("renders a dismissable window chip for a dashboard deep link", async () => {
    stubLog([SENT], 1);
    const onRangeClear = vi.fn();
    renderTimeline({ initialRange: { from: 1756684800, to: 1757289600 }, onRangeClear });
    const chip = await screen.findByRole("button", { name: /Remove window filter/ });
    await userEvent.click(chip);
    expect(onRangeClear).toHaveBeenCalledOnce();
  });

  it("constrains the query to the initialRange window with GE / LE bounds", async () => {
    const seen = stubLog([SENT], 1);
    renderTimeline({ initialRange: { from: 100, to: 200 } });
    await waitFor(() => expect(seen.length).toBeGreaterThan(0));
    // The operators matter: a pair of EQUALS on date_epoch would match nothing,
    // and a swapped GE/LE would silently invert the window.
    expect(clauses(seen[0])).toContainEqual({ type: "GE", field: "date_epoch", value: 100 });
    expect(clauses(seen[0])).toContainEqual({ type: "LE", field: "date_epoch", value: 200 });
  });

  it("pages with 10 / 25 / 50 size chips once the list overflows", async () => {
    stubLog([SENT], 42);
    renderTimeline();
    await waitFor(() => expect(screen.getByText(/Page 1 \/ 5 · 42 total/)).toBeInTheDocument());
    const controls = screen.getByText(/Page 1 \/ 5/).parentElement!;
    expect(within(controls).getByRole("button", { name: "25 per page" })).toBeInTheDocument();
    await userEvent.click(within(controls).getByRole("button", { name: "25 per page" }));
    await waitFor(() => expect(screen.getByText(/Page 1 \/ 2 · 42 total/)).toBeInTheDocument());
  });

  it("keeps the size chips reachable after a chip narrows the list below one page", async () => {
    // The controls are also the page-SIZE controls: hiding them once the
    // narrowed list fits one page strands the operator on "50 per page".
    const errors = Array.from({ length: 3 }, (_, i) => ({
      ...FAILED,
      uid: `e${i}`,
      error: `boom ${i}`,
    }));
    const sent = Array.from({ length: 39 }, (_, i) => ({ ...SENT, uid: `s${i}` }));
    stubScoped({ n1: [...sent, ...errors] });
    renderTimeline();

    await waitFor(() => expect(screen.getByText(/Page 1 \/ 5 · 42 total/)).toBeInTheDocument());
    await userEvent.click(screen.getByRole("button", { name: "50 per page" }));
    await waitFor(() => expect(screen.getByText(/Page 1 \/ 1 · 42 total/)).toBeInTheDocument());

    await userEvent.click(screen.getByRole("button", { name: "Failed" }));
    await waitFor(() => expect(screen.getByText(/Page 1 \/ 1 · 3 total/)).toBeInTheDocument());
    expect(screen.getByRole("button", { name: "50 per page" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(screen.getByRole("button", { name: "10 per page" })).toBeInTheDocument();
  });

  it("resets to page 1 when the drawer retargets to another notification", async () => {
    // The bug: page 2 of a 30-row notification became offset=10 on a 1-row
    // one, so the list came back empty under a header saying "1 delivery" —
    // with the paging controls unmounted, so there was no way back.
    stubScoped({
      n1: Array.from({ length: 30 }, (_, i) => ({
        ...SENT,
        uid: `n1-${i}`,
        alerts: [{ uid: `a${i}`, hash: `h${i}`, host: "db-01", message: `n1 row ${i}` }],
      })),
      n2: [
        {
          ...SENT,
          uid: "n2-0",
          alerts: [{ uid: "z1", hash: "zh1", host: "web-01", message: "n2 only row" }],
        },
      ],
    });
    renderRetargetable({ kind: "notification", uid: "n1" }, { kind: "notification", uid: "n2" });

    await waitFor(() => expect(screen.getByText("n1 row 0")).toBeInTheDocument());
    await userEvent.click(screen.getByRole("button", { name: "Next page" }));
    await waitFor(() => expect(screen.getByText(/Page 2 \/ 3/)).toBeInTheDocument());
    expect(screen.getByText("n1 row 10")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "retarget" }));
    // Page 1 of the new scope, not offset 10 of it.
    await waitFor(() => expect(screen.getByText("n2 only row")).toBeInTheDocument());
    expect(screen.getByText("1 delivery")).toBeInTheDocument();
    expect(screen.queryByText("No deliveries yet.")).not.toBeInTheDocument();
  });

  it("drops the previous notification's rows and counts the moment it retargets", async () => {
    // keepPreviousData across a scope change would render notification A's
    // deliveries — and A's total — under B's header, with no stale cue.
    stubScoped({
      n1: Array.from({ length: 12 }, (_, i) => ({
        ...SENT,
        uid: `n1-${i}`,
        alerts: [{ uid: `a${i}`, hash: `h${i}`, host: "db-01", message: `n1 row ${i}` }],
      })),
      n2: [],
    });
    renderRetargetable({ kind: "notification", uid: "n1" }, { kind: "notification", uid: "n2" });

    await waitFor(() => expect(screen.getByText("n1 row 0")).toBeInTheDocument());
    expect(screen.getByText("12 deliveries")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "retarget" }));
    expect(screen.queryByText("n1 row 0")).not.toBeInTheDocument();
    expect(screen.queryByText("12 deliveries")).not.toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("No deliveries yet.")).toBeInTheDocument());
  });

  it("resets the chip as well, so the new scope opens unfiltered", async () => {
    stubScoped({
      n1: [{ ...FAILED, error: "boom" }],
      n2: [
        {
          ...SENT,
          uid: "n2-0",
          alerts: [{ uid: "z1", hash: "zh1", host: "web-01", message: "n2 only row" }],
        },
      ],
    });
    renderRetargetable({ kind: "notification", uid: "n1" }, { kind: "notification", uid: "n2" });

    await waitFor(() => expect(screen.getByText("boom")).toBeInTheDocument());
    await userEvent.click(screen.getByRole("button", { name: "Failed" }));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Failed" })).toHaveAttribute(
        "aria-pressed",
        "true",
      ),
    );

    await userEvent.click(screen.getByRole("button", { name: "retarget" }));
    expect(screen.getByRole("button", { name: "All" })).toHaveAttribute("aria-pressed", "true");
    // n2 has no failures — leaving the chip on would have shown "No failed
    // deliveries." for a notification that delivered fine.
    await waitFor(() => expect(screen.getByText("n2 only row")).toBeInTheDocument());
  });

  it("clamps an out-of-range page back onto the last page", async () => {
    // Retention (or the live refetch) can shrink the list under the operator
    // without the scope changing, which the render-time reset does not cover.
    // This has to be driven by a refetch, NOT by a size chip: the chips reset
    // the page themselves, so they never let the clamp effect run.
    const rows = Array.from({ length: 30 }, (_, i) => ({ ...SENT, uid: `r${i}` }));
    stubScoped({ n1: rows });
    const { client } = renderTimeline();
    await waitFor(() => expect(screen.getByText(/Page 1 \/ 3/)).toBeInTheDocument());
    await userEvent.click(screen.getByRole("button", { name: "Next page" }));
    await userEvent.click(screen.getByRole("button", { name: "Next page" }));
    await waitFor(() => expect(screen.getByText(/Page 3 \/ 3/)).toBeInTheDocument());

    // 15 rows leaves 2 pages, so the clamp lands on page 2 rather than
    // collapsing to 1 — proof it clamped rather than reset.
    rows.length = 15;
    await act(async () => {
      await client.invalidateQueries({ queryKey: ["notificationlog"] });
    });
    await waitFor(() => expect(screen.getByText(/Page 2 \/ 2 · 15 total/)).toBeInTheDocument());
  });

  it("keeps the header on the scope total and dims the list while a chip clears", async () => {
    // Two things at once, because they are the same moment: clearing a chip
    // reuses the NARROWED page as the placeholder for the un-narrowed request,
    // so (a) the rows must be marked stale and (b) the header must not read
    // its total off them — that flashed "30 deliveries" under an All chip.
    const errors = Array.from({ length: 30 }, (_, i) => ({
      ...FAILED,
      uid: `e${i}`,
      error: `boom ${i}`,
    }));
    const sent = Array.from({ length: 20 }, (_, i) => ({ ...SENT, uid: `s${i}` }));
    stubScoped({ n1: [...sent, ...errors] }, 40);
    renderTimeline();

    await waitFor(() => expect(screen.getByText("50 deliveries")).toBeInTheDocument());
    expect(screen.getByText(/· 30 failed/)).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "Failed" }));
    await waitFor(() => expect(screen.getByText(/Page 1 \/ 3 · 30 total/)).toBeInTheDocument());
    // Narrowed list, unchanged header: the count is a statement about the
    // notification, not about the chip.
    expect(screen.getByText("50 deliveries")).toBeInTheDocument();

    // A non-default page size makes the un-narrowed key uncached, which is
    // what puts the narrowed page on screen as a placeholder.
    await userEvent.click(screen.getByRole("button", { name: "25 per page" }));
    await waitFor(() => expect(screen.getByText(/Page 1 \/ 2 · 30 total/)).toBeInTheDocument());

    await userEvent.click(screen.getByRole("button", { name: "All" }));
    await waitFor(() =>
      expect(screen.getAllByRole("list")[0]).toHaveAttribute("data-stale", "true"),
    );
    expect(screen.queryByText("30 deliveries")).not.toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("50 deliveries")).toBeInTheDocument());
    expect(screen.getAllByRole("list")[0]).not.toHaveAttribute("data-stale");
  });

  it("polls the header counts, not just the rows, while live", async () => {
    // The counts are a separate pair of limit=1 queries. Without `live` they
    // never refetched, so "142 deliveries · 3 failed" (and the alert
    // inspector's tab badge, which dedupes onto the same key) froze at mount
    // while the rows underneath kept updating.
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      let failed = 1;
      let total = 9;
      mswServer.use(
        http.get("/api/v1/notificationlog", ({ request }) => {
          const url = new URL(request.url);
          const cond = decodeQ(url.searchParams.get("q"));
          const limit = Number(url.searchParams.get("limit") ?? "10");
          const errorOnly = clauses(cond).some((c) => c.field === "status");
          const count = errorOnly ? failed : total;
          return HttpResponse.json({
            data: limit === 1 ? [] : [SENT],
            meta: { count: limit === 1 ? 0 : 1, limit, offset: 0, total: count },
          });
        }),
      );
      renderTimeline({ live: true });
      await waitFor(() => expect(screen.getByText(/· 1 failed/)).toBeInTheDocument());

      failed = 4;
      total = 12;
      await vi.advanceTimersByTimeAsync(DELIVERY_REFETCH_MS + 1000);
      await waitFor(() => expect(screen.getByText(/· 4 failed/)).toBeInTheDocument());
      expect(screen.getByText("12 deliveries")).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });

  it("has no axe violations with rows, chips and paging rendered", async () => {
    stubLog([SENT, FAILED, BATCH], 42);
    const { container } = renderTimeline();
    await waitFor(() => expect(screen.getByText("Batch · 7 alerts")).toBeInTheDocument());
    const result = await runAxe(container);
    if (result.violations.length > 0) {
      console.error(JSON.stringify(result.violations, null, 2));
    }
    expect(result.violations).toHaveLength(0);
  });
});
