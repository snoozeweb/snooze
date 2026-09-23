import { describe, expect, it } from "vitest";
import {
  formatOwnerParam,
  ownerCondition,
  parseOwnerParam,
  toggleOwner,
  UNOWNED_TOKEN,
} from "./ownerFilter";

describe("owner filter URL", () => {
  it("round-trips a selection through ?owner=", () => {
    const owners = ["alice", "bob", UNOWNED_TOKEN];
    const raw = formatOwnerParam(owners);
    expect(raw).toBe("alice,bob,~none");
    expect(parseOwnerParam(raw)).toEqual(owners);
  });

  it("drops the key for an empty selection", () => {
    expect(formatOwnerParam([])).toBeUndefined();
    expect(parseOwnerParam(undefined)).toEqual([]);
    expect(parseOwnerParam("")).toEqual([]);
  });

  it("tolerates a hand-typed URL: blanks, spaces and repeats", () => {
    expect(parseOwnerParam(" alice,,bob ,alice,")).toEqual(["alice", "bob"]);
  });

  it("toggles a token in and out, keeping selection order", () => {
    expect(toggleOwner(["alice"], "bob")).toEqual(["alice", "bob"]);
    expect(toggleOwner(["alice", "bob"], "alice")).toEqual(["bob"]);
  });
});

describe("ownerCondition", () => {
  it("is null with nothing selected", () => {
    expect(ownerCondition([])).toBeNull();
  });

  it("is a bare EQUALS for one owner", () => {
    expect(ownerCondition(["alice"])).toEqual({ type: "EQUALS", field: "owner", value: "alice" });
  });

  it("ORs several owners — multi-select widens", () => {
    expect(ownerCondition(["alice", "bob"])).toEqual({
      type: "OR",
      args: [
        { type: "EQUALS", field: "owner", value: "alice" },
        { type: "EQUALS", field: "owner", value: "bob" },
      ],
    });
  });

  it("spells Unowned as a missing key OR an empty owner", () => {
    // Clearing ownership writes "" (the pipeline's merge write cannot unset
    // it); records from before ownership have no key at all.
    expect(ownerCondition([UNOWNED_TOKEN])).toEqual({
      type: "OR",
      args: [
        { type: "NOT", arg: { type: "EXISTS", field: "owner" } },
        { type: "EQUALS", field: "owner", value: "" },
      ],
    });
  });

  it("flattens Unowned into the same OR as the named owners", () => {
    expect(ownerCondition(["alice", UNOWNED_TOKEN])).toEqual({
      type: "OR",
      args: [
        { type: "EQUALS", field: "owner", value: "alice" },
        { type: "NOT", arg: { type: "EXISTS", field: "owner" } },
        { type: "EQUALS", field: "owner", value: "" },
      ],
    });
  });
});
