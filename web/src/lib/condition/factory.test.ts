import { describe, expect, it } from "vitest";
import {
  emptyCondition,
  insertChildAtEnd,
  removeAtPath,
  replaceAtPath,
  wrapInGroup,
} from "./factory";
import type { Condition } from "./types";

describe("emptyCondition", () => {
  it("returns ALWAYS_TRUE by default", () => {
    expect(emptyCondition()).toEqual({ type: "ALWAYS_TRUE" });
  });
});

describe("wrapInGroup", () => {
  it("wraps a leaf in an AND", () => {
    const leaf: Condition = { type: "EQUALS", field: "host", value: "x" };
    expect(wrapInGroup(leaf, "AND")).toEqual({ type: "AND", args: [leaf] });
  });
});

describe("replaceAtPath", () => {
  it("replaces the root when path is empty", () => {
    const next: Condition = { type: "EQUALS", field: "a", value: "1" };
    const got = replaceAtPath({ type: "ALWAYS_TRUE" }, [], next);
    expect(got).toEqual(next);
  });

  it("replaces a child in an AND group", () => {
    const root: Condition = {
      type: "AND",
      args: [
        { type: "EQUALS", field: "a", value: "1" },
        { type: "EQUALS", field: "b", value: "2" },
      ],
    };
    const next: Condition = { type: "EQUALS", field: "c", value: "3" };
    const got = replaceAtPath(root, [1], next);
    expect(got).toEqual({
      type: "AND",
      args: [{ type: "EQUALS", field: "a", value: "1" }, next],
    });
  });

  it("does not mutate the original tree", () => {
    const root: Condition = {
      type: "AND",
      args: [{ type: "EQUALS", field: "a", value: "1" }],
    };
    const before = JSON.stringify(root);
    replaceAtPath(root, [0], { type: "EQUALS", field: "a", value: "2" });
    expect(JSON.stringify(root)).toBe(before);
  });
});

describe("removeAtPath", () => {
  it("removes a child from a group", () => {
    const root: Condition = {
      type: "AND",
      args: [
        { type: "EQUALS", field: "a", value: "1" },
        { type: "EQUALS", field: "b", value: "2" },
      ],
    };
    expect(removeAtPath(root, [0])).toEqual({
      type: "AND",
      args: [{ type: "EQUALS", field: "b", value: "2" }],
    });
  });

  it("removing the only child leaves an empty group", () => {
    const root: Condition = {
      type: "AND",
      args: [{ type: "EQUALS", field: "a", value: "1" }],
    };
    expect(removeAtPath(root, [0])).toEqual({ type: "AND", args: [] });
  });

  it("removing root yields ALWAYS_TRUE", () => {
    expect(removeAtPath({ type: "EQUALS", field: "a", value: "1" }, [])).toEqual({
      type: "ALWAYS_TRUE",
    });
  });
});

describe("insertChildAtEnd", () => {
  it("appends to a group's args", () => {
    const root: Condition = {
      type: "AND",
      args: [{ type: "EQUALS", field: "a", value: "1" }],
    };
    const leaf: Condition = { type: "EQUALS", field: "b", value: "2" };
    expect(insertChildAtEnd(root, [], leaf)).toEqual({
      type: "AND",
      args: [{ type: "EQUALS", field: "a", value: "1" }, leaf],
    });
  });
});
