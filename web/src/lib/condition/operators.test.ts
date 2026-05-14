import { describe, expect, it } from "vitest";
import { OPERATORS, defaultValueForOp, operatorLabel, valueShapeForOp } from "./operators";
import type { ConditionType } from "./types";

describe("operators", () => {
  it("exposes the full set of leaf operators (no AND/OR/NOT)", () => {
    const keys = OPERATORS.map((o) => o.type);
    expect(keys).toContain("EQUALS");
    expect(keys).toContain("MATCHES");
    expect(keys).toContain("IN");
    expect(keys).toContain("EXISTS");
    expect(keys).toContain("ALWAYS_TRUE");
    expect(keys).not.toContain("AND");
    expect(keys).not.toContain("OR");
    expect(keys).not.toContain("NOT");
  });

  it("labels are human-readable", () => {
    expect(operatorLabel("EQUALS")).toBe("equals");
    expect(operatorLabel("MATCHES")).toBe("matches");
    expect(operatorLabel("ALWAYS_TRUE")).toBe("always");
  });

  it.each([
    ["EQUALS", "string"],
    ["MATCHES", "string"],
    ["IN", "array"],
    ["LT", "number"],
    ["EXISTS", "none"],
    ["ALWAYS_TRUE", "none"],
  ] as Array<[ConditionType, ReturnType<typeof valueShapeForOp>]>)(
    "valueShapeForOp(%s) = %s",
    (op, expected) => {
      expect(valueShapeForOp(op)).toBe(expected);
    },
  );

  it("defaultValueForOp returns appropriate defaults", () => {
    expect(defaultValueForOp("EQUALS")).toBe("");
    expect(defaultValueForOp("IN")).toEqual([]);
    expect(defaultValueForOp("LT")).toBe(0);
    expect(defaultValueForOp("EXISTS")).toBeUndefined();
  });
});
