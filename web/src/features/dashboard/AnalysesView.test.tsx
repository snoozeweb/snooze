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
 * The view's one query, plus a stand-in answer for any probe that does not
 * carry the `agentic` clause — the view must not make one. The ratio against
 * the open backlog moved to the Right-now tile, so a second population here
 * would be a number with nothing to compare it to.
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

  it("lists the alert, its cause, its plan step by step, confidence and author", async () => {
    mockRecords([DISK, OOM]);
    setup();

    const rows = await screen.findAllByRole("link");
    expect(rows).toHaveLength(2);

    const disk = within(rows[0]!);
    expect(disk.getByText("srv-victoria1")).toBeInTheDocument();
    expect(disk.getByText("DiskWillFill")).toBeInTheDocument();
    expect(disk.getByText("Critical")).toBeInTheDocument();
    expect(
      disk.getByText("Kopia maintenance left 40 GB of orphaned blobs on /var"),
    ).toBeInTheDocument();
    expect(disk.getByText("High confidence")).toBeInTheDocument();
    // The plan is beside the cause now, one line per step, rather than a count
    // standing in for it.
    expect(disk.getByText("step 1")).toBeInTheDocument();
    expect(disk.getByText("step 2")).toBeInTheDocument();
    expect(disk.getByText("step 3")).toBeInTheDocument();
    expect(disk.getByText("Automatable")).toBeInTheDocument();
    expect(disk.getByText("by agent-bot")).toBeInTheDocument();

    const oom = within(rows[1]!);
    expect(oom.getByText("Low confidence")).toBeInTheDocument();
    expect(oom.getByText("step 1")).toBeInTheDocument();
    expect(oom.queryByText("step 2")).not.toBeInTheDocument();
    expect(oom.queryByText("Automatable")).not.toBeInTheDocument();
  });

  it("names the alert by its message, falling back to the rule name", async () => {
    mockRecords([
      { ...DISK, message: "/var at 94%" },
      // No message of its own: the rule name stands in for it.
      { ...OOM, labels: { alertname: "KubePodCrashLooping" } },
    ]);
    setup();

    const rows = await screen.findAllByRole("link");
    expect(within(rows[0]!).getByText("/var at 94%")).toBeInTheDocument();
    // The rule's identifier is not what fired; it does not lead the row.
    expect(within(rows[0]!).queryByText("DiskWillFill")).not.toBeInTheDocument();
    expect(within(rows[1]!).getByText("KubePodCrashLooping")).toBeInTheDocument();
  });

  it("shows a step's command and risk beside its action", async () => {
    mockRecords([
      {
        ...DISK,
        agentic: {
          ...DISK.agentic,
          remediation_plan: {
            steps: [
              { action: "Run kopia maintenance", command: "kopia maintenance run", risk: "high" },
              { action: "Open an MR", risk: "low" },
            ],
            automatable: false,
          },
        },
      },
    ]);
    setup();

    const row = within((await screen.findAllByRole("link"))[0]!);
    expect(row.getByText("kopia maintenance run")).toBeInTheDocument();
    expect(row.getByText("high")).toBeInTheDocument();
    // A step that is not a command says so by having none, not by an empty
    // code block.
    expect(row.getByText("Open an MR")).toBeInTheDocument();
    // Risk is printed only above low: a plan of LOW tags is chrome, and it
    // buries the one step that is not low.
    expect(row.getAllByText(/^(medium|high)$/)).toHaveLength(1);
    expect(row.queryByText("low")).not.toBeInTheDocument();
  });

  it("asks for the analysed population and nothing else", async () => {
    // The ratio against the open backlog belongs to the Right-now tile. A
    // second population fetched here would be a denominator with no numerator
    // on screen — and it is what used to print "7 analysed of 6 open".
    const conditions: string[] = [];
    mswServer.use(
      http.get("/api/v1/record", ({ request }) => {
        conditions.push(decodeQ(request));
        return HttpResponse.json({
          data: [DISK],
          meta: { count: 1, limit: 500, offset: 0, total: 1 },
        });
      }),
    );
    setup();

    await screen.findAllByRole("link");
    expect(conditions.length).toBeGreaterThan(0);
    for (const cond of conditions) expect(cond).toContain('"agentic"');
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
    const rows = await screen.findAllByRole("link");
    expect(rows).toHaveLength(2);
    const odd = within(rows[1]!);
    expect(odd.getByText("srv-odd")).toBeInTheDocument();
    // Cause and plan both degrade to an em-dash; either alone proves the row
    // survived its own malformed subtree.
    expect(odd.getAllByText("\u2014").length).toBeGreaterThan(0);
    expect(odd.queryByText(/confidence/i)).not.toBeInTheDocument();
    expect(screen.queryByText("No analyses yet")).not.toBeInTheDocument();
  });

  it("drops a confidence from the list when its chip is switched off", async () => {
    mockRecords([DISK, OOM]);
    const user = userEvent.setup();
    setup();

    expect(await screen.findAllByRole("link")).toHaveLength(2);
    await user.click(screen.getByRole("button", { name: "Low" }));

    const rows = screen.getAllByRole("link");
    expect(rows).toHaveLength(1);
    expect(within(rows[0]!).getByText("srv-victoria1")).toBeInTheDocument();
  });

  it("narrows to automatable plans, and says so when a filter empties the list", async () => {
    mockRecords([DISK, OOM]);
    const user = userEvent.setup();
    setup();

    expect(await screen.findAllByRole("link")).toHaveLength(2);

    await user.click(screen.getByRole("button", { name: "Yes" }));
    expect(screen.getAllByRole("link")).toHaveLength(1);
    expect(screen.getByText("srv-victoria1")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "No" }));
    expect(screen.getAllByRole("link")).toHaveLength(1);
    expect(screen.getByText("srv-legacy2")).toBeInTheDocument();

    // Automatable=no AND high-confidence-only leaves nothing.
    await user.click(screen.getByRole("button", { name: "Low" }));
    expect(screen.queryAllByRole("link")).toHaveLength(0);
    expect(screen.getByText("Nothing matches these filters")).toBeInTheDocument();
  });

  it("says what is hidden when there are more analyses than it fetches", async () => {
    mockRecords([DISK, OOM], { total: 73 });
    setup();

    expect(await screen.findByText("Showing 2 of 73 analysed alerts")).toBeInTheDocument();
  });

  it("says nothing above the list when it holds every analysed alert", async () => {
    mockRecords([DISK, OOM]);
    setup();

    await screen.findAllByRole("link");
    expect(screen.queryByText(/Showing \d+ of/)).not.toBeInTheDocument();
    expect(screen.queryByText(/newest analysis first/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/acknowledged and snoozed included/i)).not.toBeInTheDocument();
  });

  it("links each row to its alert on the All tab, opened on the Analysis tab", async () => {
    mockRecords([DISK]);
    setup();

    const rows = await screen.findAllByRole("link");
    const href = rows[0]!.getAttribute("href") ?? "";
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

// The whole row is one <a>, so base.css's `a:hover { text-decoration: underline }`
// (0-1-1) outranks a `.rowLink` (0-1-0) reset and underlines every word in the
// row, commands included — which no other table in the app does. The reset has
// to be restated on :hover, where it wins, and the hover paint has to be the
// same --bg-hover the DataTable rows use.
describe("row hover matches the app's other tables", () => {
  const css = readFileSync(
    resolve(process.cwd(), "src/features/dashboard/AnalysesView.module.css"),
    "utf8",
  );

  it("cancels the global link underline on hover", () => {
    const hover = /\.rowLink:hover\s*\{([^}]*)\}/.exec(css);
    expect(hover).not.toBeNull();
    expect(hover![1]).toMatch(/text-decoration:\s*none/);
    expect(hover![1]).toMatch(/background:\s*var\(--bg-hover\)/);
  });
});
