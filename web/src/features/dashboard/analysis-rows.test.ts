// The parsing and predicate layer behind the Analyses view. Four things are
// worth pinning down here rather than through the rendered view:
//
//   - the tile's two conditions are ONE population — its numerator and
//     denominator have to be the same set of alerts minus/plus the analysis
//     clause, or it states a ratio whose halves were measured differently —
//     and the list is that population widened by exactly the closed alerts;
//   - every DSL string this module hands to a link round-trips through the
//     parser that will read it back;
//   - a row degrades rather than disappears (the prod analysis predates every
//     optional field the contract grew);
//   - "most urgent" is an order an on-call engineer would agree with.
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { parseText } from "@/lib/condition/text";
import type { Record_ } from "@/features/alerts/types";
import {
  ALERTS_PAGE_SIZE,
  ALL_VERDICTS,
  ANALYSED_LISTED_ALERTS,
  ANALYSED_OPEN_ALERTS,
  DEFAULT_FILTERS,
  OPEN_ALERTS,
  analysedSetSearch,
  filtersActive,
  filtersFromSearch,
  matchesFilters,
  searchFromFilters,
  openAlertSearch,
  riskyStepCount,
  sortRows,
  toAnalysedRow,
  uidSearch,
  type AnalysedRow,
} from "./analysis-rows";

/** What every population leaves out: shelved alerts and the legacy negative TTL. */
const LISTED_CLAUSES = [
  { type: "NOT", arg: { type: "EQUALS", field: "state", value: "shelved" } },
  { type: "NOT", arg: { type: "LT", field: "ttl", value: 0 } },
];
/** The tile's "not finished with": the listed clauses, closed alerts out too. */
const OPEN_CLAUSES = [
  { type: "NOT", arg: { type: "EQUALS", field: "state", value: "close" } },
  ...LISTED_CLAUSES,
];

describe("the populations", () => {
  it("measures the tile's numerator and denominator over the same clauses", () => {
    expect(OPEN_ALERTS).toEqual({ type: "AND", args: OPEN_CLAUSES });
    expect(ANALYSED_OPEN_ALERTS).toEqual({
      type: "AND",
      args: [{ type: "EXISTS", field: "agentic" }, ...OPEN_CLAUSES],
    });
  });

  it("lists the same analysed alerts plus the closed ones, nothing else", () => {
    expect(ANALYSED_LISTED_ALERTS).toEqual({
      type: "AND",
      args: [{ type: "EXISTS", field: "agentic" }, ...LISTED_CLAUSES],
    });
  });
});

describe("uidSearch", () => {
  it("round-trips one uid through the search DSL", () => {
    const parsed = parseText(uidSearch("a-uid_with.dots-42"));
    expect(parsed.ok && parsed.value).toEqual({
      type: "EQUALS",
      field: "uid",
      value: "a-uid_with.dots-42",
    });
  });

  it("quotes a uid the DSL would otherwise lex as several tokens", () => {
    const parsed = parseText(uidSearch('weird "uid" with spaces'));
    expect(parsed.ok && parsed.value).toEqual({
      type: "EQUALS",
      field: "uid",
      value: 'weird "uid" with spaces',
    });
  });
});

describe("the search an opened row lands on", () => {
  it("serialises the listed population itself (closed included), and it round-trips", () => {
    const parsed = parseText(analysedSetSearch());
    expect(parsed.ok && parsed.value).toEqual(ANALYSED_LISTED_ALERTS);
  });

  it("selects the whole analysed set while it fits on one alerts page", () => {
    // So the drawer's prev/next walks the analysed alerts, not a list of one.
    expect(openAlertSearch("r-1", 1)).toBe(analysedSetSearch());
    expect(openAlertSearch("r-1", ALERTS_PAGE_SIZE)).toBe(analysedSetSearch());
  });

  it("falls back to pinning the one uid once the set outgrows a page", () => {
    // The alerts page loads one page and its drawer closes itself on a uid
    // that is not on it; past a page, the target may not be.
    expect(openAlertSearch("r-1", ALERTS_PAGE_SIZE + 1)).toBe(uidSearch("r-1"));
  });

  it("agrees with the alerts page about how big a page is", () => {
    // The constant is duplicated rather than imported (the alerts page does not
    // export it); this is the tripwire for the day the two drift apart.
    const page = readFileSync(resolve(process.cwd(), "src/features/alerts/AlertsPage.tsx"), "utf8");
    expect(page).toMatch(new RegExp(`const PAGE_SIZE = ${ALERTS_PAGE_SIZE};`));
  });
});

/** The shape the server returns for a well-formed analysis. */
const WELL_FORMED = {
  uid: "r-ok",
  host: "srv-a",
  severity: "critical",
  state: "open",
  date_epoch: 1_758_000_000,
  agentic: {
    root_cause: { summary: "Disk filled", confidence: "high" },
    remediation_plan: { steps: [{ action: "prune", risk: "low" }], automatable: true },
    analysis: { at: "2026-09-20T10:00:00Z", by: "agent-bot", source: "alert-rca" },
  },
} as unknown as Record_;

describe("toAnalysedRow — a counted record is a visible record", () => {
  it("parses a well-formed subtree", () => {
    expect(toAnalysedRow(WELL_FORMED)).toMatchObject({
      uid: "r-ok",
      summary: "Disk filled",
      headline: "Disk filled",
      body: "",
      confidence: "high",
      steps: 1,
      automatable: true,
      by: "agent-bot",
      state: "open",
      snoozed: false,
      firedAt: 1_758_000_000,
      author: { kind: "agent", tool: "alert-rca", by: "agent-bot" },
    });
  });

  it("reads the fields the contract grew: detail, caveats, status, when", () => {
    const row = toAnalysedRow({
      uid: "r-new",
      agentic: {
        root_cause: {
          summary: "Rollout lacked runAsUser",
          detail: "Revision 24 never got an available replica.",
          caveats: ["No pod logs survived"],
          evidence: ["kubectl rollout history", "caveat: node metrics were not checked"],
          confidence: "medium",
        },
        remediation_plan: {
          status: "self_resolved",
          steps: [
            { action: "Nothing to run", risk: "low", when: "now" },
            { action: "Fix the chart", risk: "medium", when: "follow_up" },
          ],
        },
      },
    } as unknown as Record_);
    expect(row?.headline).toBe("Rollout lacked runAsUser");
    expect(row?.body).toBe("Revision 24 never got an available replica.");
    // Explicit caveats first, then the legacy "caveat:" evidence line.
    expect(row?.caveats).toEqual(["No pod logs survived", "node metrics were not checked"]);
    expect(row?.status).toBe("self_resolved");
    expect(row?.plan.map((s) => s.when)).toEqual(["now", "follow_up"]);
  });

  it("recovers a headline from the long one-sentence summary prod actually carries", () => {
    const summary =
      "Collateral damage from a fleet-wide api:3.7.0 rollout on ovh (2026-09-21 ~19:05-19:23 UTC): " +
      "first wave lacked required securityContext.runAsUser:84000, so revision 24 (19:07) never got " +
      "an available replica. Recreate strategy + 1 replica meant 0 availability for ~15min, tripping " +
      "the alert. Corrective re-roll (revision 25, 19:22) added runAsUser and resolved it. " +
      "Self-recovered; not an erm10127-specific fault.";
    const row = toAnalysedRow({
      uid: "r-prod",
      agentic: { root_cause: { summary, confidence: "high" } },
    } as unknown as Record_);
    expect(row!.headline.length).toBeLessThan(summary.length);
    // Nothing the agent wrote is lost: the conclusion is in the body.
    expect(`${row!.headline} ${row!.body}`).toContain("not an erm10127-specific fault");
  });

  it("marks an analysis the alert has fired past", () => {
    const row = toAnalysedRow({
      ...WELL_FORMED,
      date_epoch: Date.parse("2026-09-20T12:00:00Z") / 1000,
    } as unknown as Record_);
    expect(row?.stale).toBe(true);
    expect(toAnalysedRow(WELL_FORMED)?.stale).toBe(false);
  });

  it("says a web-editor analysis was written by a person", () => {
    const row = toAnalysedRow({
      ...WELL_FORMED,
      agentic: { analysis: { by: "alice", source: "snooze-web" } },
    } as unknown as Record_);
    expect(row?.author).toEqual({ kind: "human", tool: "", by: "alice" });
  });

  it("reads a snoozed alert as snoozed", () => {
    expect(
      toAnalysedRow({ ...WELL_FORMED, snoozed: "maintenance" } as unknown as Record_)?.snoozed,
    ).toBe(true);
  });

  it("parses each step the panel prints beside the cause", () => {
    const row = toAnalysedRow({
      uid: "r-plan",
      host: "srv-a",
      agentic: {
        remediation_plan: {
          steps: [
            { action: "Run maintenance", command: "kopia maintenance run", risk: "high" },
            { action: "Open an MR", risk: "sideways", when: "later" },
            "written by hand, as a string",
            {},
          ],
        },
      },
    } as unknown as Record_);
    expect(row?.steps).toBe(4);
    expect(row?.plan).toEqual([
      {
        action: "Run maintenance",
        command: "kopia maintenance run",
        risk: "high",
        when: undefined,
      },
      // A level this app cannot paint is dropped rather than printed raw — the
      // step still lists. Same for a `when` outside the two the contract names.
      { action: "Open an MR", command: "", risk: "", when: undefined },
      { action: "written by hand, as a string", command: "", risk: "", when: undefined },
      // A step that names nothing still holds its position: the plan's order is
      // its meaning, and the count beside it is the array's length.
      { action: "", command: "", risk: "", when: undefined },
    ]);
  });

  it("keeps a record whose confidence is missing or outside the three levels", () => {
    const row = toAnalysedRow({
      uid: "r-odd",
      host: "srv-b",
      agentic: { root_cause: { summary: "Cause without a level" } },
    } as unknown as Record_);
    expect(row).toBeDefined();
    expect(row?.summary).toBe("Cause without a level");
    // No level to badge — the row still lists, the badge is simply absent.
    expect(row?.confidence).toBeUndefined();
    expect(row?.steps).toBe(0);
  });

  it("keeps a record whose root cause is a bare string", () => {
    const row = toAnalysedRow({
      uid: "r-str",
      host: "srv-c",
      agentic: { root_cause: "written by hand, as a string" },
    } as unknown as Record_);
    expect(row?.summary).toBe("written by hand, as a string");
    expect(row?.headline).toBe("written by hand, as a string");
  });

  it("keeps a record whose analysis subtree carries nothing this panel prints", () => {
    const row = toAnalysedRow({
      uid: "r-empty",
      host: "srv-d",
      agentic: { analysis: { by: "agent-bot" } },
    } as unknown as Record_);
    expect(row).toBeDefined();
    expect(row?.summary).toBe("—");
    expect(row?.headline).toBe("—");
    expect(row?.status).toBeUndefined();
    expect(row?.by).toBe("agent-bot");
  });

  it("still drops a record with no analysis subtree at all — it was never counted", () => {
    expect(toAnalysedRow({ uid: "r-none", host: "srv-e" } as unknown as Record_)).toBeUndefined();
  });
});

describe("riskyStepCount", () => {
  it("counts only the steps whose risk changes who may run them", () => {
    const row = toAnalysedRow({
      uid: "r",
      agentic: {
        remediation_plan: {
          steps: [
            { action: "a", risk: "low" },
            { action: "b", risk: "medium" },
            { action: "c", risk: "high" },
            { action: "d" },
          ],
        },
      },
    } as unknown as Record_)!;
    expect(riskyStepCount(row.plan)).toBe(2);
  });
});

describe("matchesFilters", () => {
  const base = toAnalysedRow(WELL_FORMED)!;
  const row = (patch: Partial<AnalysedRow>): AnalysedRow => ({ ...base, ...patch });
  const everything = { ...DEFAULT_FILTERS, verdict: ALL_VERDICTS };

  it("opens on the verdicts that still ask something of a person", () => {
    expect(filtersActive(DEFAULT_FILTERS)).toBe(false);
    expect(matchesFilters(row({ status: "action_required" }), DEFAULT_FILTERS)).toBe(true);
    expect(matchesFilters(row({ status: "monitoring" }), DEFAULT_FILTERS)).toBe(true);
    // No verdict has not said it is finished: kept, never hidden silently.
    expect(matchesFilters(row({ status: undefined }), DEFAULT_FILTERS)).toBe(true);
    expect(matchesFilters(row({ status: "resolved" }), DEFAULT_FILTERS)).toBe(false);
    expect(matchesFilters(row({ status: "self_resolved" }), DEFAULT_FILTERS)).toBe(false);
  });

  it("lets everything through with every verdict ticked, degraded rows included", () => {
    expect(filtersActive(everything)).toBe(true);
    for (const status of [
      "action_required",
      "monitoring",
      "self_resolved",
      "resolved",
      undefined,
    ]) {
      expect(matchesFilters(row({ status: status as AnalysedRow["status"] }), everything)).toBe(
        true,
      );
    }
    expect(matchesFilters(row({ confidence: undefined }), everything)).toBe(true);
  });

  it("reads confidence as a threshold, not a set", () => {
    const mediumUp = { ...everything, confidence: "medium" as const };
    const highOnly = { ...everything, confidence: "high" as const };
    expect(matchesFilters(row({ confidence: "high" }), mediumUp)).toBe(true);
    expect(matchesFilters(row({ confidence: "medium" }), mediumUp)).toBe(true);
    expect(matchesFilters(row({ confidence: "low" }), mediumUp)).toBe(false);
    expect(matchesFilters(row({ confidence: "medium" }), highOnly)).toBe(false);
    // A row with no level cannot answer "at least medium".
    expect(matchesFilters(row({ confidence: undefined }), mediumUp)).toBe(false);
  });

  it("answers the automatable question both ways", () => {
    expect(matchesFilters(row({ automatable: true }), { ...everything, automatable: "yes" })).toBe(
      true,
    );
    expect(matchesFilters(row({ automatable: true }), { ...everything, automatable: "no" })).toBe(
      false,
    );
  });

  it("filters on a set of verdicts", () => {
    const finished = { ...everything, verdict: ["self_resolved", "resolved"] as const };
    expect(matchesFilters(row({ status: "resolved" }), finished)).toBe(true);
    expect(matchesFilters(row({ status: "self_resolved" }), finished)).toBe(true);
    expect(matchesFilters(row({ status: "monitoring" }), finished)).toBe(false);
    expect(matchesFilters(row({ status: undefined }), finished)).toBe(false);
    // Nothing ticked is a real state: nothing matches.
    expect(matchesFilters(row({ status: "resolved" }), { ...everything, verdict: [] })).toBe(false);
  });

  it("shows closed alerts unless asked not to", () => {
    const closedRow = row({ state: "close" });
    expect(matchesFilters(closedRow, everything)).toBe(true);
    const hide = { ...everything, closed: "hide" as const };
    expect(filtersActive({ ...DEFAULT_FILTERS, closed: "hide" })).toBe(true);
    expect(matchesFilters(closedRow, hide)).toBe(false);
    expect(matchesFilters(row({ state: "ack" }), hide)).toBe(true);
  });
});

describe("the filters in the URL", () => {
  it("omits every param at its default", () => {
    expect(searchFromFilters(DEFAULT_FILTERS)).toEqual({});
    expect(filtersFromSearch({})).toEqual(DEFAULT_FILTERS);
  });

  it("round-trips every non-default filter", () => {
    const f = {
      confidence: "high" as const,
      automatable: "no" as const,
      verdict: ["resolved", "action_required"] as const,
      closed: "hide" as const,
    };
    const search = searchFromFilters(f);
    // One canonical spelling, whatever order the chips were ticked in.
    expect(search).toEqual({
      confidence: "high",
      automatable: "no",
      verdict: "action_required,resolved",
      closed: "hide",
    });
    expect(filtersFromSearch(search)).toEqual({ ...f, verdict: ["action_required", "resolved"] });
  });

  it("keeps an empty verdict set as an empty string", () => {
    expect(searchFromFilters({ ...DEFAULT_FILTERS, verdict: [] })).toEqual({ verdict: "" });
    expect(filtersFromSearch({ verdict: "" }).verdict).toEqual([]);
  });
});

describe("sortRows", () => {
  const base = toAnalysedRow(WELL_FORMED)!;
  const row = (uid: string, patch: Partial<AnalysedRow>): AnalysedRow => ({
    ...base,
    uid,
    ...patch,
  });

  it("puts a closed alert after every alert still in play, whatever it fired at", () => {
    const rows = [
      row("critical-closed", { severity: "critical", state: "close", firedAt: 999 }),
      row("warning-open", { severity: "warning", firedAt: 100 }),
      row("warning-closed", { severity: "warning", state: "close", firedAt: 500 }),
    ];
    expect(sortRows(rows, "urgent").map((r) => r.uid)).toEqual([
      "warning-open",
      "critical-closed",
      "warning-closed",
    ]);
  });

  it("puts the most urgent first: severity, then unacknowledged, then newest fire", () => {
    const rows = [
      row("warning-new", { severity: "warning", firedAt: 300 }),
      row("critical-acked", { severity: "critical", state: "ack", firedAt: 900 }),
      row("critical-old", { severity: "critical", firedAt: 100 }),
      row("critical-snoozed", { severity: "critical", snoozed: true, firedAt: 950 }),
      row("critical-new", { severity: "critical", firedAt: 200 }),
      row("unknown", { severity: "whatever", firedAt: 999 }),
      row("critical-escalated", { severity: "critical", state: "esc", firedAt: 150 }),
    ];
    expect(sortRows(rows, "urgent").map((r) => r.uid)).toEqual([
      "critical-new",
      // Re-escalated is still unacknowledged — it is back in somebody's queue.
      "critical-escalated",
      "critical-old",
      // Acknowledged and snoozed are held, newest fire first among them.
      "critical-snoozed",
      "critical-acked",
      "warning-new",
      // An unrecognised severity is not evidence of urgency.
      "unknown",
    ]);
  });

  it("orders by the analysis itself on request, newest first", () => {
    const rows = [
      row("a", { analysedAt: 100 }),
      row("none", { analysedAt: undefined }),
      row("b", { analysedAt: 300 }),
    ];
    expect(sortRows(rows, "recent").map((r) => r.uid)).toEqual(["b", "a", "none"]);
  });

  it("does not reorder the input it was handed", () => {
    const rows = [row("x", { severity: "warning" }), row("y", { severity: "critical" })];
    sortRows(rows, "urgent");
    expect(rows.map((r) => r.uid)).toEqual(["x", "y"]);
  });
});
