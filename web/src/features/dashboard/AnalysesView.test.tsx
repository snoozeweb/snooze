import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { render, screen, within } from "@testing-library/react";
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
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { mswServer } from "@/tests/msw/server";
import { AnalysesView } from "./AnalysesView";

type RecordFixture = {
  uid: string;
  host: string;
  severity?: string;
  confidence: "high" | "medium" | "low";
  summary: string;
  steps?: number;
  automatable?: boolean;
  alertname?: string;
};

function analysedRecord(f: RecordFixture) {
  return {
    uid: f.uid,
    host: f.host,
    severity: f.severity ?? "critical",
    state: "open",
    date_epoch: 1_700_000_000,
    ...(f.alertname === undefined ? {} : { labels: { alertname: f.alertname } }),
    agentic: {
      root_cause: { summary: f.summary, confidence: f.confidence },
      remediation_plan: {
        steps: Array.from({ length: f.steps ?? 1 }, (_, i) => ({
          action: `step ${i + 1}`,
          risk: "low",
        })),
        automatable: f.automatable ?? false,
      },
      analysis: { at: "2026-09-20T10:00:00Z", by: "agent-bot", source: "alert-rca" },
    },
  };
}

/** The condition a /record request carried, as readable JSON. */
function decodeQ(request: Request): string {
  const q = new URL(request.url).searchParams.get("q") ?? "";
  if (q === "") return "";
  const b64 = q.replace(/-/g, "+").replace(/_/g, "/");
  return atob(b64 + "=".repeat((4 - (b64.length % 4)) % 4));
}

/**
 * Both queries the view makes, off one handler, told apart by the condition
 * they carry rather than by their limit — the count and the page are the same
 * predicate at two page sizes.
 *
 * `activeTotal` answers an ACTIVE_ALERTS-shaped probe (the one with the `ack`
 * and `snoozed` clauses). The view must never ask for it: that population
 * excludes the acked and snoozed rows the analysed list keeps, which is how
 * "7 analysed of 6 open" got printed.
 */
function mockRecords(
  records: unknown[],
  { total = records.length, openTotal = 42, activeTotal = 6 } = {},
) {
  mswServer.use(
    http.get("/api/v1/record", ({ request }) => {
      const cond = decodeQ(request);
      if (!cond.includes('"agentic"')) {
        const total = cond.includes('"ack"') ? activeTotal : openTotal;
        return HttpResponse.json({ data: [], meta: { count: 0, limit: 1, offset: 0, total } });
      }
      return HttpResponse.json({
        data: records,
        meta: { count: records.length, limit: 50, offset: 0, total },
      });
    }),
  );
}

/** Mirrors the real alerts route's search allowlist for the keys the view writes. */
function alertsValidateSearch(raw: Record<string, unknown>): {
  tab?: string;
  search?: string;
  record?: string;
  analysis?: boolean;
} {
  const out: { tab?: string; search?: string; record?: string; analysis?: boolean } = {};
  if (typeof raw["tab"] === "string") out.tab = raw["tab"];
  if (typeof raw["search"] === "string") out.search = raw["search"];
  if (typeof raw["record"] === "string") out.record = raw["record"];
  if (typeof raw["analysis"] === "boolean") out.analysis = raw["analysis"];
  return out;
}

function setup() {
  const root = createRootRoute({ component: () => <Outlet /> });
  const alertsRoute = createRoute({
    getParentRoute: () => root,
    path: "/web/alerts",
    component: () => <div>alerts page</div>,
    validateSearch: alertsValidateSearch,
  });
  const dashboardRoute = createRoute({
    getParentRoute: () => root,
    path: "/web/dashboard",
    component: () => <AnalysesView />,
  });
  const tree = root.addChildren([alertsRoute, dashboardRoute]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: ["/web/dashboard"] }),
  } as any);
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <RouterProvider router={router as Parameters<typeof RouterProvider>[0]["router"]} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

const DISK = analysedRecord({
  uid: "r-disk",
  host: "srv-victoria1",
  alertname: "DiskWillFill",
  confidence: "high",
  summary: "Kopia maintenance left 40 GB of orphaned blobs on /var",
  steps: 3,
  automatable: true,
});

const OOM = analysedRecord({
  uid: "r-oom",
  host: "srv-legacy2",
  confidence: "low",
  summary: "Container restarted; no evidence of the trigger survived",
  steps: 1,
  automatable: false,
});

describe("AnalysesView", () => {
  it("says where analyses come from when there are none", async () => {
    mockRecords([]);
    setup();

    expect(await screen.findByText("No analyses yet")).toBeInTheDocument();
    expect(
      screen.getByText(/alert-rca agent loop writes a root cause and remediation plan/i),
    ).toBeInTheDocument();
  });

  it("lists the alert, its cause, confidence, step count, automatable mark and author", async () => {
    mockRecords([DISK, OOM]);
    setup();

    const rows = await screen.findAllByRole("listitem");
    expect(rows).toHaveLength(2);

    const disk = within(rows[0]!);
    expect(disk.getByText("srv-victoria1")).toBeInTheDocument();
    expect(disk.getByText("DiskWillFill")).toBeInTheDocument();
    expect(disk.getByText("Critical")).toBeInTheDocument();
    expect(
      disk.getByText("Kopia maintenance left 40 GB of orphaned blobs on /var"),
    ).toBeInTheDocument();
    expect(disk.getByText("High confidence")).toBeInTheDocument();
    // The count and its noun are separate nodes: the noun is spoken and
    // printed only once the row stacks and the column header is gone.
    expect(disk.getByText("3")).toHaveTextContent("3 steps");
    expect(disk.getByText("Automatable")).toBeInTheDocument();
    expect(disk.getByText("agent-bot")).toBeInTheDocument();

    const oom = within(rows[1]!);
    expect(oom.getByText("Low confidence")).toBeInTheDocument();
    expect(oom.getByText("1")).toHaveTextContent("1 step");
    expect(oom.queryByText("Automatable")).not.toBeInTheDocument();
  });

  it("reads its count against the population it was counted over, not the needs-attention queue", async () => {
    // The analysed list keeps acked and snoozed rows; ACTIVE_ALERTS drops
    // them. Measuring the two halves of the ratio differently is what printed
    // "7 analysed of 6 open" as a normal end state.
    mockRecords([DISK, OOM], { openTotal: 42, activeTotal: 6 });
    setup();

    expect(await screen.findByText("2")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "42 open" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "6 open" })).not.toBeInTheDocument();
    // And it says so, because "open" on its own reads as the default tab.
    expect(screen.getByText(/acknowledged and snoozed included/i)).toBeInTheDocument();
  });

  it("links the open half at the same population minus what is already explained", async () => {
    mockRecords([DISK, OOM], { openTotal: 42 });
    setup();

    const link = await screen.findByRole("link", { name: "42 open" });
    const href = decodeURIComponent(link.getAttribute("href") ?? "");
    expect(href).toContain("/web/alerts");
    // EXISTS is postfix in the search DSL, so "no analysis" is `NOT agentic?`.
    expect(href).toContain("NOT agentic?");
    // …over the same "still in play" clauses the count was measured with, so
    // the link lands on exactly the rows the view does not list.
    expect(href).toContain('NOT state = "close"');
    expect(href).toContain('NOT state = "shelved"');
    expect(href).toContain("NOT ttl < 0");
  });

  it("does not hide a record it counted just because the subtree is malformed", async () => {
    const MALFORMED = {
      uid: "r-odd",
      host: "srv-odd",
      severity: "warning",
      state: "open",
      date_epoch: 1_700_000_000,
      labels: { alertname: "HandWritten" },
      agentic: { analysis: { at: "2026-09-20T11:00:00Z", by: "hand" } },
    };
    mockRecords([DISK, MALFORMED]);
    setup();

    // The headline counts two, so the list shows two — the second degraded to
    // an em-dash cause with no confidence badge rather than dropped.
    const rows = await screen.findAllByRole("listitem");
    expect(rows).toHaveLength(2);
    const odd = within(rows[1]!);
    expect(odd.getByText("srv-odd")).toBeInTheDocument();
    expect(odd.getByText("\u2014")).toBeInTheDocument();
    expect(odd.queryByText(/confidence/i)).not.toBeInTheDocument();
    expect(screen.queryByText("No analyses yet")).not.toBeInTheDocument();
  });

  it("drops a confidence from the list when its chip is switched off", async () => {
    mockRecords([DISK, OOM]);
    const user = userEvent.setup();
    setup();

    expect(await screen.findAllByRole("listitem")).toHaveLength(2);
    await user.click(screen.getByRole("button", { name: "Low" }));

    const rows = screen.getAllByRole("listitem");
    expect(rows).toHaveLength(1);
    expect(within(rows[0]!).getByText("srv-victoria1")).toBeInTheDocument();
  });

  it("narrows to automatable plans, and says so when a filter empties the list", async () => {
    mockRecords([DISK, OOM]);
    const user = userEvent.setup();
    setup();

    expect(await screen.findAllByRole("listitem")).toHaveLength(2);

    await user.click(screen.getByRole("button", { name: "Yes" }));
    expect(screen.getAllByRole("listitem")).toHaveLength(1);
    expect(screen.getByText("srv-victoria1")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "No" }));
    expect(screen.getAllByRole("listitem")).toHaveLength(1);
    expect(screen.getByText("srv-legacy2")).toBeInTheDocument();

    // Automatable=no AND high-confidence-only leaves nothing.
    await user.click(screen.getByRole("button", { name: "Low" }));
    expect(screen.queryAllByRole("listitem")).toHaveLength(0);
    expect(screen.getByText("Nothing matches these filters")).toBeInTheDocument();
  });

  it("says what is hidden when there are more analyses than it fetches", async () => {
    mockRecords([DISK, OOM], { total: 73 });
    setup();

    expect(await screen.findByText("Top 50 of 73")).toBeInTheDocument();
  });

  it("links each row to its alert on the All tab, opened on the Analysis tab", async () => {
    mockRecords([DISK]);
    setup();

    const rows = await screen.findAllByRole("listitem");
    const href = within(rows[0]!).getByRole("link").getAttribute("href") ?? "";
    expect(href).toContain("/web/alerts");
    // The All tab, because an analysed alert is often already acknowledged and
    // the drawer closes itself when the uid isn't on the page it lands on.
    expect(href).toContain("tab=all");
    expect(href).toContain("record=r-disk");
    expect(href).toContain("analysis=true");
    // And it pins the table to that one row: the alerts page fetches the
    // newest 50 by date_epoch and closes a drawer whose uid isn't among them —
    // which is precisely the row this view ranks first (old alert, fresh
    // analysis).
    expect(decodeURIComponent(href)).toContain('search=uid = "r-disk"');
  });
});

// Same amber-on-amber trap as the view switch: an active chip is filled with
// --accent-solid, which is the same #ffb000 as --focus-ring in dark
// (styles/theme.dark.css). The global ring (styles/base.css) sits on the card
// behind the chip and reads; a bespoke one pulled to 1px off the fill does not.
describe("focus is visible on the chip that is on", () => {
  const css = readFileSync(
    resolve(process.cwd(), "src/features/dashboard/AnalysesView.module.css"),
    "utf8",
  );

  it("does not re-declare the global focus ring on the chip", () => {
    expect(css).not.toMatch(/\.chip:focus-visible/);
  });

  it("gives the active chip a ring drawn in the on-accent ink", () => {
    expect(css).toMatch(/\.chip\[data-active="true"\]:focus-visible/);
    expect(css).toMatch(/inset 0 0 0 2px var\(--accent-solid-fg\)/);
  });
});
