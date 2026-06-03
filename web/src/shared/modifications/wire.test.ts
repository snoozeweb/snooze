import { describe, expect, it } from "vitest";
import type { Modification } from "./types";
import { fromWire, modificationsFromWire, modificationsToWire, toWire } from "./wire";

describe("modifications wire", () => {
  it("round-trips each variant", () => {
    const cases: Modification[] = [
      { type: "set", field: "environment", value: "prod" },
      { type: "delete", field: "old_tag" },
      { type: "array_append", field: "tags", value: "alpha" },
      { type: "array_delete", field: "tags", value: "alpha" },
      { type: "regex_parse", field: "message", pattern: "^(?P<svc>\\w+):" },
      { type: "regex_sub", field: "host", pattern: "^web-", replace: "frontend-" },
      { type: "kv_set", field: "owner", dict: "host_owner", key: "host" },
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

  it("encodes REGEX_SUB with src=dst (Python's 5-arg positional form)", () => {
    expect(toWire({ type: "regex_sub", field: "host", pattern: "^web-", replace: "f-" })).toEqual([
      "REGEX_SUB",
      "host",
      "host",
      "^web-",
      "f-",
    ]);
  });

  it("decodes legacy REGEX_SUB with separate src/dst fields", () => {
    // Python form: ["REGEX_SUB", src_field, dst_field, pattern, replace]
    expect(fromWire(["REGEX_SUB", "raw_host", "host", "\\.egerie\\.eu", ""])).toEqual({
      type: "regex_sub",
      field: "host",
      pattern: "\\.egerie\\.eu",
      replace: "",
    });
  });

  it("decodes KV_SET in the Python-era 4-arg form", () => {
    expect(fromWire(["KV_SET", "host_owner", "host", "owner"])).toEqual({
      type: "kv_set",
      field: "owner",
      dict: "host_owner",
      key: "host",
    });
  });

  it("encodes KV_SET as [KV_SET, dict, key, out_field]", () => {
    expect(toWire({ type: "kv_set", field: "owner", dict: "host_owner", key: "host" })).toEqual([
      "KV_SET",
      "host_owner",
      "host",
      "owner",
    ]);
  });

  it("decodes legacy 3-arg KV_SET written by the early Go rewrite", () => {
    // Old bug-for-bug shape: ["KV_SET", out_field, key]. We pull the out_field
    // off entry[1] (where every other op puts `field`) and accept the missing
    // dict as empty — round-tripping through toWire will then re-emit the
    // canonical 4-arg form.
    expect(fromWire(["KV_SET", "owner", "host"])).toEqual({
      type: "kv_set",
      field: "owner",
      dict: "",
      key: "host",
    });
  });

  it("decodes REGEX_PARSE that the previous editor dropped", () => {
    expect(fromWire(["REGEX_PARSE", "message", "^(?P<svc>\\w+):"])).toEqual({
      type: "regex_parse",
      field: "message",
      pattern: "^(?P<svc>\\w+):",
    });
  });

  it("drops unknown ops from fromWire", () => {
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
    const decoded = modificationsFromWire([...wire, ["MYSTERY"], "garbage"]);
    expect(decoded).toEqual(mods);
  });
});
