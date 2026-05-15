import { describe, expect, it } from "vitest";
import type { Modification } from "./types";
import {
  fromWire,
  modificationsFromWire,
  modificationsToWire,
  toWire,
} from "./wire";

describe("modifications wire", () => {
  it("round-trips each variant", () => {
    const cases: Modification[] = [
      { type: "set", field: "environment", value: "prod" },
      { type: "delete", field: "old_tag" },
      { type: "array_append", field: "tags", value: "alpha" },
      { type: "regex_sub", field: "host", pattern: "^web-", replace: "frontend-" },
    ];
    for (const c of cases) {
      const wire = toWire(c);
      const back = fromWire(wire);
      expect(back).toEqual(c);
    }
  });

  it("encodes SET in the positional form", () => {
    expect(toWire({ type: "set", field: "a", value: "1" })).toEqual(["SET", "a", "1"]);
  });

  it("drops unknown ops from fromWire", () => {
    expect(fromWire(["KV_SET", "f", "k"])).toBeNull();
    expect(fromWire(["MYSTERY"])).toBeNull();
    expect(fromWire([])).toBeNull();
    expect(fromWire("not-an-array")).toBeNull();
  });

  it("modificationsToWire / modificationsFromWire skip malformed entries", () => {
    const mods: Modification[] = [
      { type: "set", field: "a", value: "1" },
      { type: "delete", field: "b" },
    ];
    const wire = modificationsToWire(mods);
    expect(wire).toEqual([
      ["SET", "a", "1"],
      ["DELETE", "b"],
    ]);
    // Add an unknown op and a non-array — both should be dropped on decode.
    const decoded = modificationsFromWire([...wire, ["KV_SET", "x", "y"], "garbage"]);
    expect(decoded).toEqual(mods);
  });
});
