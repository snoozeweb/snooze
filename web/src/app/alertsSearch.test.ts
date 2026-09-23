// The alerts route's search-param contract. Every other route's validator is a
// straight type filter; this one carries rules — a closed set of panes, a
// legacy boolean alias for one of them, and a key that is meaningless when
// empty — and a URL is the one input a human (or another product) types by
// hand.
import { describe, expect, it } from "vitest";
import { validateAlertsSearch } from "./alertsSearch";

describe("validateAlertsSearch", () => {
  it("keeps a named inspector pane", () => {
    for (const pane of ["flow", "analysis", "deliveries", "record"]) {
      expect(validateAlertsSearch({ record: "r1", pane })).toEqual({ record: "r1", pane });
    }
  });

  it("drops ?pane=timeline and unknown panes — Timeline is the absence of the param", () => {
    expect(validateAlertsSearch({ record: "r1", pane: "timeline" })).toEqual({ record: "r1" });
    expect(validateAlertsSearch({ record: "r1", pane: "nonsense" })).toEqual({ record: "r1" });
  });

  it("reads every truthy spelling of the legacy ?analysis= as pane=analysis", () => {
    // Older dashboard links and bookmarks still carry it.
    for (const raw of [true, "true", 1, "1"]) {
      expect(validateAlertsSearch({ record: "r1", analysis: raw })).toEqual({
        record: "r1",
        pane: "analysis",
      });
    }
  });

  it("drops a falsy legacy ?analysis= instead of round-tripping it as URL litter", () => {
    expect(validateAlertsSearch({ record: "r1", analysis: false })).toEqual({ record: "r1" });
    expect(validateAlertsSearch({ record: "r1", analysis: "false" })).toEqual({ record: "r1" });
    expect(validateAlertsSearch({ record: "r1", analysis: "yes" })).toEqual({ record: "r1" });
  });

  it("lets an explicit pane win over the legacy ?analysis=", () => {
    expect(validateAlertsSearch({ record: "r1", pane: "record", analysis: "1" })).toEqual({
      record: "r1",
      pane: "record",
    });
  });

  it("rejects an empty ?record=", () => {
    // Not "open nothing" but a key that matches no row — the same reason the
    // notifications route refuses an empty ?details=.
    expect(validateAlertsSearch({ record: "" })).toEqual({});
    expect(validateAlertsSearch({ record: "r1" })).toEqual({ record: "r1" });
  });

  it("keeps the rest of the contract intact", () => {
    expect(
      validateAlertsSearch({
        tab: "all",
        env: "e1,e2",
        owner: "alice,~none",
        page: "3",
        orderby: "date_epoch",
        asc: "false",
        search: "host = srv-1",
      }),
    ).toEqual({
      tab: "all",
      env: "e1,e2",
      owner: "alice,~none",
      page: 3,
      orderby: "date_epoch",
      asc: false,
      search: "host = srv-1",
    });
  });
});
