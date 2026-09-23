// The parsing and predicate layer behind the Analyses view. Two things are
// worth pinning down here rather than through the rendered view:
//
//   - the three conditions are ONE population — the tile's denominator, the
//     list's numerator and the "what is left" link have to be the same set of
//     alerts minus/plus the analysis clause, or the view states a ratio whose
//     halves were measured differently;
//   - every DSL string this module hands to a link round-trips through the
//     parser that will read it back.
import { describe, expect, it } from "vitest";
import { parseText } from "@/lib/condition/text";
import { CONFIDENCE_LEVELS } from "@/features/alerts/analysis/enums";
import type { Record_ } from "@/features/alerts/types";
import {
  ANALYSED_OPEN_ALERTS,
  OPEN_ALERTS,
  matchesFilters,
  toAnalysedRow,
  uidSearch,
} from "./analysis-rows";

/** The "not finished with" clauses every one of the three conditions carries. */
const OPEN_CLAUSES = [
  { type: "NOT", arg: { type: "EQUALS", field: "state", value: "close" } },
  { type: "NOT", arg: { type: "EQUALS", field: "state", value: "shelved" } },
  { type: "NOT", arg: { type: "LT", field: "ttl", value: 0 } },
];

describe("one population", () => {
  it("measures the list and the tile's denominator over the same clauses", () => {
    expect(OPEN_ALERTS).toEqual({ type: "AND", args: OPEN_CLAUSES });
    expect(ANALYSED_OPEN_ALERTS).toEqual({
      type: "AND",
      args: [{ type: "EXISTS", field: "agentic" }, ...OPEN_CLAUSES],
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

/** The shape the server returns for a well-formed analysis. */
const WELL_FORMED = {
  uid: "r-ok",
  host: "srv-a",
  severity: "critical",
  agentic: {
    root_cause: { summary: "Disk filled", confidence: "high" },
    remediation_plan: { steps: [{ action: "prune" }], automatable: true },
    analysis: { at: "2026-09-20T10:00:00Z", by: "agent-bot" },
  },
} as unknown as Record_;

describe("toAnalysedRow — a counted record is a visible record", () => {
  it("parses a well-formed subtree", () => {
    expect(toAnalysedRow(WELL_FORMED)).toMatchObject({
      uid: "r-ok",
      summary: "Disk filled",
      confidence: "high",
      steps: 1,
      automatable: true,
      by: "agent-bot",
    });
  });

  it("parses each step the panel prints beside the cause", () => {
    const row = toAnalysedRow({
      uid: "r-plan",
      host: "srv-a",
      agentic: {
        remediation_plan: {
          steps: [
            { action: "Run maintenance", command: "kopia maintenance run", risk: "high" },
            { action: "Open an MR", risk: "sideways" },
            "written by hand, as a string",
            {},
          ],
        },
      },
    } as unknown as Record_);
    expect(row?.steps).toBe(4);
    expect(row?.plan).toEqual([
      { action: "Run maintenance", command: "kopia maintenance run", risk: "high" },
      // A level this app cannot paint is dropped rather than printed raw — the
      // step still lists.
      { action: "Open an MR", command: "", risk: "" },
      { action: "written by hand, as a string", command: "", risk: "" },
      // A step that names nothing still holds its position: the plan's order is
      // its meaning, and the count beside it is the array's length.
      { action: "", command: "", risk: "" },
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
  });

  it("keeps a record whose analysis subtree carries nothing this panel prints", () => {
    const row = toAnalysedRow({
      uid: "r-empty",
      host: "srv-d",
      agentic: { analysis: { by: "agent-bot" } },
    } as unknown as Record_);
    expect(row).toBeDefined();
    expect(row?.summary).toBe("—");
    expect(row?.by).toBe("agent-bot");
  });

  it("still drops a record with no analysis subtree at all — it was never counted", () => {
    expect(toAnalysedRow({ uid: "r-none", host: "srv-e" } as unknown as Record_)).toBeUndefined();
  });
});

describe("matchesFilters with a degraded row", () => {
  const degraded = { ...toAnalysedRow(WELL_FORMED)!, confidence: undefined };

  it("lists it while the confidence chips are untouched", () => {
    expect(matchesFilters(degraded, CONFIDENCE_LEVELS, "any")).toBe(true);
  });

  it("hides it once the operator narrows to actual levels", () => {
    expect(matchesFilters(degraded, ["high", "medium"], "any")).toBe(false);
  });

  it("still answers the automatable chip", () => {
    expect(matchesFilters(degraded, CONFIDENCE_LEVELS, "no")).toBe(false);
    expect(matchesFilters(degraded, CONFIDENCE_LEVELS, "yes")).toBe(true);
  });
});
