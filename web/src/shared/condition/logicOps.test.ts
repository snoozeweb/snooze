import { describe, expect, it } from "vitest";
import type { Condition } from "@/lib/condition/types";
import { switchLogicOp } from "./logicOps";

const leaf = (): Condition => ({ type: "EQUALS", field: "x", value: "1" });
const a: Condition = { type: "EQUALS", field: "a", value: "1" };
const b: Condition = { type: "EQUALS", field: "b", value: "2" };
const c: Condition = { type: "EQUALS", field: "c", value: "3" };

describe("switchLogicOp", () => {
  it("switching a multi-child AND to NOT keeps only the first child (no extra parent group)", () => {
    expect(switchLogicOp({ type: "AND", args: [a, b, c] }, "NOT", leaf)).toEqual({
      type: "NOT",
      arg: a,
    });
  });

  it("does the same for an OR group", () => {
    expect(switchLogicOp({ type: "OR", args: [a, b] }, "NOT", leaf)).toEqual({
      type: "NOT",
      arg: a,
    });
  });

  it("a single-child group becomes a plain NOT of that child", () => {
    expect(switchLogicOp({ type: "AND", args: [a] }, "NOT", leaf)).toEqual({ type: "NOT", arg: a });
  });

  it("switching to AND/OR pads to at least two args", () => {
    const out = switchLogicOp({ type: "NOT", arg: a }, "AND", leaf);
    expect(out).toEqual({ type: "AND", args: [a, leaf()] });
  });

  it("switching NOT(OR(a,b)) to AND unwraps to AND(a,b)", () => {
    expect(switchLogicOp({ type: "NOT", arg: { type: "OR", args: [a, b] } }, "AND", leaf)).toEqual({
      type: "AND",
      args: [a, b],
    });
  });
});
