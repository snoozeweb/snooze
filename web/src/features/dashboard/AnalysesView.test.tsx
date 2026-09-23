import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import axe from "axe-core";
import type { AxeResults } from "axe-core";
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

type StepFixture = {
  action: string;
  risk?: string;
  command?: string;
  when?: string;
};

type RecordFixture = {
  uid: string;
  host: string;
  severity?: string;
  state?: string;
  snoozed?: string;
  firedAt?: number;
  message?: string;
  alertname?: string;
  confidence?: "high" | "medium" | "low";
  summary: string;
  detail?: string;
  caveats?: string[];
  evidence?: string[];
  status?: string;
  steps?: number | StepFixture[];
  automatable?: boolean;
  at?: string;
  by?: string;
  source?: string;
};

/** Epoch seconds for an ISO time — fixtures read better as dates. */
const epoch = (iso: string) => Date.parse(iso) / 1000;

function analysedRecord(f: RecordFixture) {
  const steps: StepFixture[] =
    typeof f.steps === "object"
      ? f.steps
      : Array.from({ length: f.steps ?? 1 }, (_, i) => ({ action: `step ${i + 1}`, risk: "low" }));
  return {
    uid: f.uid,
    host: f.host,
    severity: f.severity ?? "critical",
    state: f.state ?? "open",
    // Older than the analysis by default: an analysis the alert has fired past
    // is its own case below.
    date_epoch: f.firedAt ?? epoch("2026-09-20T09:00:00Z"),
    ...(f.snoozed === undefined ? {} : { snoozed: f.snoozed }),
    ...(f.message === undefined ? {} : { message: f.message }),
    ...(f.alertname === undefined ? {} : { labels: { alertname: f.alertname } }),
    agentic: {
      root_cause: {
        summary: f.summary,
        confidence: f.confidence ?? "high",
        ...(f.detail === undefined ? {} : { detail: f.detail }),
        ...(f.caveats === undefined ? {} : { caveats: f.caveats }),
        ...(f.evidence === undefined ? {} : { evidence: f.evidence }),
      },
      remediation_plan: {
        steps,
        automatable: f.automatable ?? false,
        ...(f.status === undefined ? {} : { status: f.status }),
      },
      analysis: {
        at: f.at ?? "2026-09-20T10:00:00Z",
        by: f.by ?? "agent-bot",
        source: f.source ?? "alert-rca",
      },
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
 * carry the `agentic` clause — the view must not make one.
 */
function mockRecords(records: unknown[], { total = records.length } = {}) {
  mswServer.use(
    http.get("/api/v1/record", ({ request }) => {
      if (!decodeQ(request).includes('"agentic"')) {
        return HttpResponse.json({ data: [], meta: { count: 0, limit: 1, offset: 0, total: 42 } });
      }
      return HttpResponse.json({
        data: records,
        meta: { count: records.length, limit: 500, offset: 0, total },
      });
    }),
  );
}

/** Mirrors the real alerts route's search allowlist for the keys the view writes. */
function alertsValidateSearch(raw: Record<string, unknown>): {
  tab?: string;
  search?: string;
  record?: string;
  pane?: string;
} {
  const out: { tab?: string; search?: string; record?: string; pane?: string } = {};
  if (typeof raw["tab"] === "string") out.tab = raw["tab"];
  if (typeof raw["search"] === "string") out.search = raw["search"];
  if (typeof raw["record"] === "string") out.record = raw["record"];
  if (typeof raw["pane"] === "string") out.pane = raw["pane"];
  return out;
}

/** Mirrors the dashboard route's allowlist (router.tsx) for the keys the view reads. */
function dashboardValidateSearch(raw: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  if (raw["view"] === "analyses") out["view"] = "analyses";
  if (raw["sort"] === "recent") out["sort"] = "recent";
  return out;
}

function setup(initialEntry = "/web/dashboard?view=analyses") {
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
    validateSearch: dashboardValidateSearch,
  });
  const tree = root.addChildren([alertsRoute, dashboardRoute]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: [initialEntry] }),
  } as any);
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const utils = render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <RouterProvider router={router as Parameters<typeof RouterProvider>[0]["router"]} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return { ...utils, router, client };
}

/** The rows, in the order they are on screen. */
async function rows() {
  return screen.findAllByRole("article");
}

/** The host names the rows lead with, top to bottom. */
function order(articles: HTMLElement[]): string[] {
  return articles.map(
    (a) => within(a).getByRole("heading", { level: 3 }).querySelector("[data-host]")!.textContent!,
  );
}

const DISK = analysedRecord({
  uid: "r-disk",
  host: "srv-victoria1",
  alertname: "DiskWillFill",
  confidence: "high",
  summary: "Kopia maintenance left 40 GB of orphaned blobs on /var",
  steps: [
    { action: "Run kopia maintenance", command: "kopia maintenance run --full", risk: "medium" },
    { action: "Open an MR", risk: "low" },
    { action: "Prune the snapshots", command: "kopia snapshot prune", risk: "low" },
  ],
  automatable: true,
});

const OOM = analysedRecord({
  uid: "r-oom",
  host: "srv-legacy2",
  severity: "warning",
  confidence: "low",
  summary: "Container restarted; no evidence of the trigger survived",
  steps: 1,
  automatable: false,
});

/** The one real prod analysis: none of the new fields, a 430-char "sentence". */
const PROD_SUMMARY =
  "Collateral damage from a fleet-wide api:3.7.0 rollout on ovh (2026-09-21 ~19:05-19:23 UTC): " +
  "first wave lacked required securityContext.runAsUser:84000, so revision 24 (19:07) never got " +
  "an available replica. Recreate strategy + 1 replica meant 0 availability for ~15min, tripping " +
  "the alert. Corrective re-roll (revision 25, 19:22) added runAsUser and resolved it. " +
  "Self-recovered; not an erm10127-specific fault.";

const PROD = analysedRecord({
  uid: "r-prod",
  host: "K8S ovh",
  message: "Deployment down: erm10127/erm10127-api (ovh)",
  summary: PROD_SUMMARY,
  steps: [
    { action: "No remediation required on erm10127-api itself", risk: "low" },
    { action: "Update the ovh deployment chart", risk: "low" },
    { action: "Add a canary namespace step", risk: "medium" },
    { action: "Review Recreate+1-replica deployments", risk: "medium" },
  ],
  by: "snooze",
});

describe("AnalysesView", () => {
  it("says how analyses arrive when there are none", async () => {
    mockRecords([]);
    setup();

    expect(await screen.findByText("No analyses yet")).toBeInTheDocument();
    expect(
      screen.getByText(
        "The alert-rca agent writes an analysis onto open alerts it investigates; you can also write one from an alert's Analysis tab.",
      ),
    ).toBeInTheDocument();
  });

  describe("a row", () => {
    it("is an article named by its alert, whose heading is the link — not one giant link", async () => {
      mockRecords([{ ...DISK, message: "/var at 94%" }]);
      setup();

      const [row] = await rows();
      expect(row).toHaveAccessibleName(/srv-victoria1.*\/var at 94%/);
      const heading = within(row!).getByRole("heading", { level: 3 });
      const link = within(heading).getByRole("link");
      expect(link).toHaveTextContent("srv-victoria1");
      expect(link).toHaveTextContent("/var at 94%");
      // Nothing structural lives inside a link any more.
      for (const a of within(row!).getAllByRole("link")) {
        expect(a.querySelector("ol, ul, li, button")).toBeNull();
      }
    });

    it("names the alert by its message, falling back to the rule name", async () => {
      mockRecords([
        { ...DISK, message: "/var at 94%" },
        // No message of its own: the rule name stands in for it.
        { ...OOM, labels: { alertname: "KubePodCrashLooping" } },
      ]);
      setup();

      const [disk, oom] = await rows();
      expect(within(disk!).getByText("/var at 94%")).toBeInTheDocument();
      // The rule's identifier is not what fired; it does not lead the row.
      expect(within(disk!).queryByText("DiskWillFill")).not.toBeInTheDocument();
      expect(within(oom!).getByText("KubePodCrashLooping")).toBeInTheDocument();
    });

    it("says what state the alert is in and when it fired", async () => {
      mockRecords([
        { ...DISK, state: "ack" },
        { ...OOM, snoozed: "maintenance-window" },
      ]);
      setup();

      const [disk, oom] = await rows();
      expect(within(disk!).getByText("Acknowledged")).toBeInTheDocument();
      expect(within(disk!).getByText("fired")).toBeInTheDocument();
      expect(within(oom!).getByText("Open")).toBeInTheDocument();
      expect(within(oom!).getByText("Snoozed")).toBeInTheDocument();
    });

    it("leads with the verdict and a headline, then who wrote it and how far to trust it", async () => {
      mockRecords([
        analysedRecord({
          uid: "r-new",
          host: "srv-new",
          summary: "Rollout lacked runAsUser",
          detail: "Revision 24 never got an available replica.",
          caveats: ["No pod logs survived"],
          evidence: ["kubectl rollout history", "caveat: node metrics were not checked"],
          confidence: "medium",
          status: "self_resolved",
        }),
      ]);
      setup();

      const row = within((await rows())[0]!);
      expect(row.getByText("Self-resolved")).toBeInTheDocument();
      expect(row.getByText("Rollout lacked runAsUser")).toBeInTheDocument();
      // The body waits for Expand.
      expect(
        row.queryByText("Revision 24 never got an available replica."),
      ).not.toBeInTheDocument();
      expect(row.getByText("AI analysis")).toBeInTheDocument();
      expect(row.getByText("alert-rca")).toBeInTheDocument();
      expect(row.getByText("analysed")).toBeInTheDocument();
      expect(row.getByText("Medium confidence")).toBeInTheDocument();
      // Explicit caveats and the legacy "caveat:" evidence line, counted.
      expect(row.getByText("2 caveats")).toBeInTheDocument();
    });

    it("says a person wrote a web-editor analysis", async () => {
      mockRecords([
        {
          ...DISK,
          agentic: {
            ...DISK.agentic,
            analysis: { ...DISK.agentic.analysis, by: "alice", source: "snooze-web" },
          },
        },
      ]);
      setup();

      const row = within((await rows())[0]!);
      expect(row.getByText("Written by alice")).toBeInTheDocument();
      expect(row.queryByText("AI analysis")).not.toBeInTheDocument();
    });

    it("flags an analysis the alert has fired past", async () => {
      mockRecords([
        { ...DISK, date_epoch: epoch("2026-09-20T12:00:00Z") },
        { ...OOM, uid: "r-fresh" },
      ]);
      setup();

      const [stale, fresh] = await rows();
      expect(within(stale!).getByText("Fired again since")).toBeInTheDocument();
      expect(within(fresh!).queryByText("Fired again since")).not.toBeInTheDocument();
    });

    it("summarises the plan collapsed: size, risk, the first step, automatable", async () => {
      mockRecords([DISK, OOM]);
      setup();

      const [disk, oom] = await rows();
      expect(within(disk!).getByText("Plan: 3 steps · 1 medium/high risk")).toBeInTheDocument();
      expect(within(disk!).getByText("Run kopia maintenance")).toBeInTheDocument();
      expect(within(disk!).queryByText("Open an MR")).not.toBeInTheDocument();
      expect(within(disk!).getByText("Automatable")).toBeInTheDocument();

      expect(within(oom!).getByText("Plan: 1 step")).toBeInTheDocument();
      expect(within(oom!).queryByText("Automatable")).not.toBeInTheDocument();
      // Confidence is ink, not a traffic light, and "low" says so in words.
      expect(within(oom!).getByText("Low confidence")).toBeInTheDocument();
    });

    it("splits a grouped plan into what to run now and follow-ups", async () => {
      mockRecords([
        analysedRecord({
          uid: "r-grouped",
          host: "srv-grouped",
          summary: "Replica fell behind",
          steps: [
            { action: "Restart the replica", risk: "low", when: "now" },
            { action: "Raise the WAL retention", risk: "high", when: "follow_up" },
            { action: "Add a lag alert", risk: "low", when: "follow_up" },
          ],
        }),
      ]);
      const user = userEvent.setup();
      setup();

      const row = (await rows())[0]!;
      expect(within(row).getByText("Restart the replica")).toBeInTheDocument();
      expect(within(row).getByText("Follow-ups: 2 · 1 medium/high risk")).toBeInTheDocument();

      await user.click(within(row).getByRole("button", { name: "Expand" }));
      expect(within(row).getByRole("heading", { name: "Now" })).toBeInTheDocument();
      expect(within(row).getByRole("heading", { name: "Follow-up" })).toBeInTheDocument();
      expect(within(row).getByText("Raise the WAL retention")).toBeInTheDocument();
    });
  });

  describe("expanding a row", () => {
    it("shows the whole analysis: body, caveats, every step, commands to copy", async () => {
      mockRecords([
        {
          ...DISK,
          agentic: {
            ...DISK.agentic,
            root_cause: {
              ...DISK.agentic.root_cause,
              detail: "The weekly job crashed mid-run and left the blobs behind.",
              caveats: ["Could not read the job's own log"],
            },
          },
        },
      ]);
      const user = userEvent.setup();
      setup();

      const row = (await rows())[0]!;
      const toggle = within(row).getByRole("button", { name: "Expand" });
      expect(toggle).toHaveAttribute("aria-expanded", "false");
      expect(document.getElementById(toggle.getAttribute("aria-controls")!)).not.toBeNull();

      await user.click(toggle);
      expect(within(row).getByRole("button", { name: "Collapse" })).toHaveAttribute(
        "aria-expanded",
        "true",
      );
      expect(
        within(row).getByText("The weekly job crashed mid-run and left the blobs behind."),
      ).toBeInTheDocument();
      expect(within(row).getByText("Could not read the job's own log")).toBeInTheDocument();
      expect(within(row).getByText("Open an MR")).toBeInTheDocument();
      expect(within(row).getByText("Prune the snapshots")).toBeInTheDocument();

      // The shared risk rule: low carries no mark, medium/high a spelled-out badge.
      expect(within(row).getByText("Medium risk")).toBeInTheDocument();
      expect(within(row).queryByText("Low risk")).not.toBeInTheDocument();

      expect(within(row).getByText("kopia maintenance run --full")).toBeInTheDocument();
      const copies = within(row).getAllByRole("button", { name: "Copy command" });
      expect(copies).toHaveLength(2);
      await user.click(copies[0]!);
      expect(await navigator.clipboard.readText()).toBe("kopia maintenance run --full");
    });

    it("keeps the conclusion of a long one-sentence summary reachable", async () => {
      mockRecords([PROD]);
      const user = userEvent.setup();
      setup();

      const row = (await rows())[0]!;
      // Collapsed, the headline is the first sentence — not a 430-char paragraph.
      expect(within(row).queryByText(PROD_SUMMARY)).not.toBeInTheDocument();
      expect(within(row).getByText(/^Collateral damage from a fleet-wide/)).toBeInTheDocument();

      await user.click(within(row).getByRole("button", { name: "Expand" }));
      expect(within(row).getByText(/not an erm10127-specific fault\.$/)).toBeInTheDocument();
    });

    it("keeps a row expanded across a refetch and a reorder", async () => {
      // Analysed after DISK, so "Newest analysis" moves it from second to first.
      const LATER = {
        ...OOM,
        agentic: {
          ...OOM.agentic,
          analysis: { ...OOM.agentic.analysis, at: "2026-09-20T11:00:00Z" },
        },
      };
      mockRecords([DISK, LATER]);
      const user = userEvent.setup();
      const { client } = setup();

      const before = await rows();
      expect(order(before)).toEqual(["srv-victoria1", "srv-legacy2"]);
      await user.click(within(before[1]!).getByRole("button", { name: "Expand" }));

      await act(() => client.invalidateQueries());
      await user.click(screen.getByRole("radio", { name: "Newest analysis" }));

      const after = await rows();
      expect(order(after)).toEqual(["srv-legacy2", "srv-victoria1"]);
      const again = after[0]!;
      expect(within(again).getByRole("button", { name: "Collapse" })).toHaveAttribute(
        "aria-expanded",
        "true",
      );
    });
  });

  describe("order", () => {
    const WARN_NEW = analysedRecord({
      uid: "r-warn",
      host: "srv-warn",
      severity: "warning",
      summary: "w",
      firedAt: epoch("2026-09-20T09:30:00Z"),
      at: "2026-09-20T11:00:00Z",
    });
    const CRIT_ACKED = analysedRecord({
      uid: "r-acked",
      host: "srv-acked",
      state: "ack",
      summary: "a",
      firedAt: epoch("2026-09-20T09:50:00Z"),
      at: "2026-09-20T09:55:00Z",
    });
    const CRIT_OPEN = analysedRecord({
      uid: "r-open",
      host: "srv-open",
      summary: "o",
      firedAt: epoch("2026-09-20T08:00:00Z"),
      at: "2026-09-20T08:30:00Z",
    });

    it("leads with the most urgent: severity, then unacknowledged, then newest fire", async () => {
      // Server order is by analysis time; the screen is not.
      mockRecords([WARN_NEW, CRIT_ACKED, CRIT_OPEN]);
      setup();

      expect(order(await rows())).toEqual(["srv-open", "srv-acked", "srv-warn"]);
      expect(screen.getByRole("radio", { name: "Most urgent" })).toHaveAttribute(
        "aria-checked",
        "true",
      );
    });

    it("sorts by newest analysis on request, and keeps that in the URL", async () => {
      mockRecords([CRIT_OPEN, WARN_NEW, CRIT_ACKED]);
      const user = userEvent.setup();
      const { router } = setup();
      await rows();

      await user.click(screen.getByRole("radio", { name: "Newest analysis" }));
      expect(order(screen.getAllByRole("article"))).toEqual(["srv-warn", "srv-acked", "srv-open"]);
      expect(router.state.location.search).toMatchObject({ sort: "recent" });

      // Back to the default drops the param rather than spelling it out.
      await user.click(screen.getByRole("radio", { name: "Most urgent" }));
      expect(router.state.location.search).not.toHaveProperty("sort");
    });

    it("makes a re-sort a history step, so Back restores the previous order", async () => {
      mockRecords([CRIT_OPEN, WARN_NEW, CRIT_ACKED]);
      const user = userEvent.setup();
      const { router } = setup();
      await rows();

      await user.click(screen.getByRole("radio", { name: "Newest analysis" }));
      expect(router.state.location.search).toMatchObject({ sort: "recent" });

      act(() => router.history.back());
      await waitFor(() => expect(router.state.location.search).not.toHaveProperty("sort"));
      expect(order(screen.getAllByRole("article"))).toEqual(["srv-open", "srv-acked", "srv-warn"]);
    });

    it("opens on the sort a shared link carries", async () => {
      mockRecords([CRIT_OPEN, WARN_NEW, CRIT_ACKED]);
      setup("/web/dashboard?view=analyses&sort=recent");

      expect(order(await rows())).toEqual(["srv-warn", "srv-acked", "srv-open"]);
    });
  });

  describe("filters", () => {
    it("narrows by a confidence threshold, as a radio group with a readable state", async () => {
      mockRecords([DISK, OOM]);
      const user = userEvent.setup();
      setup();
      await rows();

      const confidence = screen.getByRole("radiogroup", { name: "Confidence" });
      const any = within(confidence).getByRole("radio", { name: "Any" });
      expect(any).toHaveAttribute("aria-checked", "true");
      expect(screen.getByText("2 analyses")).toBeInTheDocument();

      await user.click(within(confidence).getByRole("radio", { name: "Medium+" }));
      expect(any).toHaveAttribute("aria-checked", "false");
      expect(order(screen.getAllByRole("article"))).toEqual(["srv-victoria1"]);
      expect(screen.getByText("1 of 2 analyses")).toBeInTheDocument();
    });

    it("moves the selection with the arrow keys, one tab stop per group", async () => {
      mockRecords([DISK, OOM]);
      const user = userEvent.setup();
      setup();
      await rows();

      const confidence = screen.getByRole("radiogroup", { name: "Confidence" });
      const [any, medium, high] = within(confidence).getAllByRole("radio");
      expect(any).toHaveAttribute("tabindex", "0");
      expect(medium).toHaveAttribute("tabindex", "-1");

      act(() => any!.focus());
      await user.keyboard("{ArrowRight}");
      expect(medium).toHaveFocus();
      expect(medium).toHaveAttribute("aria-checked", "true");
      await user.keyboard("{ArrowRight}");
      expect(high).toHaveAttribute("aria-checked", "true");
      // Wraps, as a radio group does.
      await user.keyboard("{ArrowRight}");
      expect(any).toHaveAttribute("aria-checked", "true");
    });

    it("narrows to automatable plans, and offers a way out when a filter empties the list", async () => {
      mockRecords([DISK, OOM]);
      const user = userEvent.setup();
      setup();
      await rows();

      const automatable = screen.getByRole("radiogroup", { name: "Automatable" });
      await user.click(within(automatable).getByRole("radio", { name: "Yes" }));
      expect(order(screen.getAllByRole("article"))).toEqual(["srv-victoria1"]);

      await user.click(within(automatable).getByRole("radio", { name: "No" }));
      expect(order(screen.getAllByRole("article"))).toEqual(["srv-legacy2"]);

      // Automatable=no AND high-confidence-only leaves nothing.
      const confidence = screen.getByRole("radiogroup", { name: "Confidence" });
      await user.click(within(confidence).getByRole("radio", { name: "High" }));
      expect(screen.queryAllByRole("article")).toHaveLength(0);
      expect(screen.getByText("Nothing matches these filters")).toBeInTheDocument();

      await user.click(screen.getByRole("button", { name: "Clear filters" }));
      expect(screen.getAllByRole("article")).toHaveLength(2);
      expect(within(confidence).getByRole("radio", { name: "Any" })).toHaveAttribute(
        "aria-checked",
        "true",
      );
    });

    it("offers a verdict filter only once some analysis states a verdict", async () => {
      mockRecords([DISK, OOM]);
      const { unmount } = setup();
      await rows();
      expect(screen.queryByRole("radiogroup", { name: "Verdict" })).not.toBeInTheDocument();
      unmount();

      mockRecords([
        analysedRecord({ uid: "r-act", host: "srv-act", summary: "a", status: "action_required" }),
        analysedRecord({ uid: "r-fix", host: "srv-fix", summary: "f", status: "resolved" }),
        OOM,
      ]);
      const user = userEvent.setup();
      setup();
      await rows();
      const verdict = screen.getByRole("radiogroup", { name: "Verdict" });
      await user.click(within(verdict).getByRole("radio", { name: "Action required" }));
      expect(order(screen.getAllByRole("article"))).toEqual(["srv-act"]);
      // A fix somebody applied is its own verdict, not folded into "Self-resolved".
      await user.click(within(verdict).getByRole("radio", { name: "Resolved" }));
      expect(order(screen.getAllByRole("article"))).toEqual(["srv-fix"]);
    });
  });

  describe("keyboard", () => {
    it("moves between rows with J/K and the arrows, one tab stop for the list", async () => {
      mockRecords([DISK, OOM]);
      const user = userEvent.setup();
      setup();

      const [first, second] = await rows();
      expect(first).toHaveAttribute("tabindex", "0");
      expect(second).toHaveAttribute("tabindex", "-1");

      act(() => first!.focus());
      await user.keyboard("j");
      expect(second).toHaveFocus();
      expect(second).toHaveAttribute("tabindex", "0");
      expect(first).toHaveAttribute("tabindex", "-1");
      await user.keyboard("k");
      expect(first).toHaveFocus();
      await user.keyboard("{ArrowDown}");
      expect(second).toHaveFocus();
      await user.keyboard("{ArrowUp}");
      expect(first).toHaveFocus();

      // A modified J is somebody else's shortcut.
      await user.keyboard("{Control>}j{/Control}");
      expect(first).toHaveFocus();
    });

    it("toggles the focused row with E or Space, and opens its alert with Enter", async () => {
      mockRecords([DISK, OOM]);
      const user = userEvent.setup();
      const { router } = setup();

      const [first] = await rows();
      act(() => first!.focus());
      await user.keyboard("e");
      expect(within(first!).getByRole("button", { name: "Collapse" })).toHaveAttribute(
        "aria-expanded",
        "true",
      );
      await user.keyboard(" ");
      expect(within(first!).getByRole("button", { name: "Expand" })).toHaveAttribute(
        "aria-expanded",
        "false",
      );

      await user.keyboard("{Enter}");
      expect(await screen.findByText("alerts page")).toBeInTheDocument();
      expect(router.state.location.pathname).toBe("/web/alerts");
      expect(router.state.location.search).toMatchObject({ record: "r-disk", pane: "analysis" });
    });
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

    await rows();
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

    // The tile counts two, so the list shows two — the second degraded to an
    // em-dash headline with no confidence meter rather than dropped.
    const [, odd] = await rows();
    expect(within(odd!).getByText("srv-odd")).toBeInTheDocument();
    expect(within(odd!).getAllByText("—").length).toBeGreaterThan(0);
    expect(within(odd!).queryByText(/confidence/i)).not.toBeInTheDocument();
    expect(within(odd!).getByText("No remediation plan")).toBeInTheDocument();
    // Nothing to expand into.
    expect(within(odd!).queryByRole("button", { name: "Expand" })).not.toBeInTheDocument();
  });

  it("says what is hidden when there are more analyses than it fetches", async () => {
    mockRecords([DISK, OOM], { total: 73 });
    setup();

    expect(await screen.findByText("Showing 2 of 73 analysed alerts")).toBeInTheDocument();
  });

  it("says nothing about truncation when it holds every analysed alert", async () => {
    mockRecords([DISK, OOM]);
    setup();

    await rows();
    expect(screen.queryByText(/Showing \d+ of/)).not.toBeInTheDocument();
  });

  describe("opening a row", () => {
    it("lands on its alert on the All tab, on the Analysis tab, among the analysed set", async () => {
      mockRecords([DISK, OOM]);
      setup();

      const [row] = await rows();
      const heading = within(row!).getByRole("heading", { level: 3 });
      for (const link of [
        within(heading).getByRole("link"),
        within(row!).getByRole("link", { name: "Open alert" }),
      ]) {
        const href = decodeURIComponent(link.getAttribute("href") ?? "");
        expect(href).toContain("/web/alerts");
        // The All tab, because an analysed alert is often already acknowledged
        // and the drawer closes itself when the uid isn't on the page it lands on.
        expect(href).toContain("tab=all");
        expect(href).toContain("record=r-disk");
        expect(href).toContain("pane=analysis");
        // The whole analysed set fits on one alerts page, so the table shows
        // all of it and the drawer's prev/next walks the analysed alerts.
        expect(href).toContain('search=agentic? AND (NOT state = "close")');
      }
    });

    it("pins the one uid once the analysed set outgrows an alerts page", async () => {
      mockRecords([DISK, OOM], { total: 73 });
      setup();

      const [row] = await rows();
      const href = decodeURIComponent(
        within(row!).getByRole("link", { name: "Open alert" }).getAttribute("href") ?? "",
      );
      expect(href).toContain('search=uid = "r-disk"');
    });
  });

  it("has no axe violations, collapsed or expanded", async () => {
    mockRecords([DISK, OOM, PROD]);
    const user = userEvent.setup();
    const { container } = setup();
    const [first] = await rows();
    await user.click(within(first!).getByRole("button", { name: "Expand" }));

    const result = await new Promise<AxeResults>((ok, fail) => {
      // Contrast needs a canvas jsdom does not have; it is checked in the
      // browser (the e2e tour), not here.
      axe.run(
        container,
        { rules: { "color-contrast": { enabled: false } } },
        (err: Error | null, res: AxeResults) => (err ? fail(err) : ok(res)),
      );
    });
    if (result.violations.length > 0) console.error(JSON.stringify(result.violations, null, 2));
    expect(result.violations).toHaveLength(0);
  });
});

describe("AnalysesView styles", () => {
  const sheets = ["AnalysesView", "AnalysisRow", "ChoiceGroup"].map((name) => ({
    name,
    css: readFileSync(resolve(process.cwd(), `src/features/dashboard/${name}.module.css`), "utf8"),
  }));

  // The old chips all started filled amber, so "on" and "off" were the same
  // picture. A selected chip is an outline + check; nothing is a solid fill.
  it.each(sheets)("$name never fills anything with the solid accent", ({ css }) => {
    expect(css).not.toMatch(/accent-solid/);
  });

  // The old sheet declared `.steps` twice, the second silently winning.
  it.each(sheets)("$name declares each rule block once", ({ css }) => {
    const selectors = [...css.matchAll(/^(\.[A-Za-z][\w-]*)\s*\{/gm)].map((m) => m[1]);
    const dupes = selectors.filter((s, i) => selectors.indexOf(s) !== i);
    expect(dupes).toEqual([]);
  });

  // The row is no longer a link, so there is no `a:hover` underline to cancel
  // and no whole-row hover paint pretending the row is one target.
  it.each(sheets)("$name does not paint the row as a single hover target", ({ css }) => {
    expect(css).not.toMatch(/\.rowLink/);
  });
});
