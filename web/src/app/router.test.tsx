// The alerts route's search-param contract. Every other route's validator is a
// straight type filter; this one carries rules — two spellings of a boolean,
// and a key that is meaningless when empty — and a URL is the one input a
// human (or another product) types by hand.
import { afterAll, describe, expect, it } from "vitest";
import { stopSessionRefresh } from "@/lib/auth/session";
import { validateAlertsSearch } from "./router";

// Importing the router module arms the background token refresh; tear it down
// so it doesn't outlive the suite.
afterAll(() => stopSessionRefresh());

describe("validateAlertsSearch", () => {
  it("accepts every truthy spelling of ?analysis=", () => {
    for (const raw of [true, "true", 1, "1"]) {
      expect(validateAlertsSearch({ record: "r1", analysis: raw })).toEqual({
        record: "r1",
        analysis: true,
      });
    }
  });

  it("drops ?analysis=false instead of round-tripping it as URL litter", () => {
    // `false` is the default open, so keeping it writes `?analysis=false` back
    // into every link the page builds and shares from then on.
    expect(validateAlertsSearch({ record: "r1", analysis: false })).toEqual({ record: "r1" });
    expect(validateAlertsSearch({ record: "r1", analysis: "false" })).toEqual({ record: "r1" });
    expect(validateAlertsSearch({ record: "r1", analysis: "yes" })).toEqual({ record: "r1" });
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
        page: "3",
        orderby: "date_epoch",
        asc: "false",
        search: "host = srv-1",
      }),
    ).toEqual({
      tab: "all",
      env: "e1,e2",
      page: 3,
      orderby: "date_epoch",
      asc: false,
      search: "host = srv-1",
    });
  });
});
