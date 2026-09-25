// The dashboard route's search-param contract: every default is the absence of
// its key, a hand-edited value can't poison the page, and the verdict ids this
// eager module spells out cannot drift from the plan statuses they name.
import { describe, expect, it } from "vitest";
import { PLAN_STATUSES } from "@/features/alerts/analysis/verdict";
import {
  DEFAULT_VERDICT_IDS,
  VERDICT_IDS,
  parseVerdictParam,
  validateDashboardSearch,
} from "./dashboardSearch";

describe("validateDashboardSearch", () => {
  it("keeps the existing keys as before", () => {
    expect(
      validateDashboardSearch({
        view: "analyses",
        sort: "recent",
        range: "custom",
        from: "1000",
        to: 2000,
      }),
    ).toEqual({ view: "analyses", sort: "recent", range: "custom", from: 1000, to: 2000 });
    expect(validateDashboardSearch({ view: "overview", sort: "urgent", range: "2d" })).toEqual({});
  });

  it("keeps a non-default filter and drops defaults and unknown values", () => {
    expect(
      validateDashboardSearch({
        confidence: "high",
        automatable: "no",
        closed: "hide",
        verdict: "resolved,self_resolved",
      }),
    ).toEqual({
      confidence: "high",
      automatable: "no",
      closed: "hide",
      verdict: "self_resolved,resolved",
    });
    expect(
      validateDashboardSearch({ confidence: "any", automatable: "maybe", closed: "show" }),
    ).toEqual({});
  });

  it("drops a verdict set equal to the default, whatever its spelling", () => {
    expect(validateDashboardSearch({ verdict: "no_verdict,monitoring,action_required" })).toEqual(
      {},
    );
  });

  it("keeps an empty verdict set: every chip unticked is a real state", () => {
    expect(validateDashboardSearch({ verdict: "" })).toEqual({ verdict: "" });
  });
});

describe("parseVerdictParam", () => {
  it("ignores unknown ids, and falls back to the default when none is known", () => {
    expect(parseVerdictParam("resolved,bogus")).toEqual(["resolved"]);
    // A typo must not empty the list.
    expect(parseVerdictParam("bogus")).toBeUndefined();
    expect(parseVerdictParam(42)).toBeUndefined();
    expect(parseVerdictParam(undefined)).toBeUndefined();
  });

  it("dedupes and orders the one way the URL spells it", () => {
    expect(parseVerdictParam("resolved, resolved ,action_required")).toEqual([
      "action_required",
      "resolved",
    ]);
  });
});

describe("the verdict ids", () => {
  it("are the plan statuses, in their order, then no_verdict", () => {
    expect(VERDICT_IDS).toEqual([...PLAN_STATUSES, "no_verdict"]);
  });

  it("default to the verdicts that still need a person", () => {
    expect(DEFAULT_VERDICT_IDS).toEqual(["action_required", "monitoring", "no_verdict"]);
  });
});
