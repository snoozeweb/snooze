// web/src/lib/condition/text.test.ts
import { describe, it, expect } from "vitest";
import { encodeText, parseText } from "./text";
import type { Condition } from "./types";

describe("encodeText", () => {
  it("ALWAYS_TRUE → empty string", () => {
    expect(encodeText({ type: "ALWAYS_TRUE" })).toBe("");
  });
  it("EQUALS with simple value", () => {
    expect(encodeText({ type: "EQUALS", field: "host", value: "srv-1" })).toBe(`host = "srv-1"`);
  });
  it("EQUALS with value needing quoting", () => {
    expect(encodeText({ type: "EQUALS", field: "msg", value: 'a "b" c' })).toBe(
      `msg = "a \\"b\\" c"`,
    );
  });
  it("MATCHES uses ~", () => {
    expect(encodeText({ type: "MATCHES", field: "msg", value: "^ERR" })).toBe(`msg ~ "^ERR"`);
  });
  it("SEARCH is bare value", () => {
    expect(encodeText({ type: "SEARCH", field: "", value: "needle" } as Condition)).toBe(
      `"needle"`,
    );
  });
  it("EXISTS uses field?", () => {
    expect(encodeText({ type: "EXISTS", field: "trace_id" })).toBe(`trace_id?`);
  });
  it("IN uses field IN [array]", () => {
    expect(encodeText({ type: "IN", field: "sev", value: ["err", "crit"] })).toBe(
      `sev IN ["err", "crit"]`,
    );
  });
  it("LT / GT use < / >", () => {
    expect(encodeText({ type: "LT", field: "n", value: 5 })).toBe(`n < 5`);
    expect(encodeText({ type: "GE", field: "n", value: 5 })).toBe(`n >= 5`);
  });
  it("AND joins with AND", () => {
    expect(
      encodeText({
        type: "AND",
        args: [
          { type: "EQUALS", field: "a", value: "1" },
          { type: "EQUALS", field: "b", value: "2" },
        ],
      }),
    ).toBe(`a = "1" AND b = "2"`);
  });
  it("OR wraps nested AND in parens", () => {
    expect(
      encodeText({
        type: "OR",
        args: [
          { type: "EQUALS", field: "a", value: "1" },
          {
            type: "AND",
            args: [
              { type: "EQUALS", field: "b", value: "2" },
              { type: "EQUALS", field: "c", value: "3" },
            ],
          },
        ],
      }),
    ).toBe(`a = "1" OR (b = "2" AND c = "3")`);
  });
  it("NOT prefixes with NOT", () => {
    expect(encodeText({ type: "NOT", arg: { type: "EXISTS", field: "x" } })).toBe(`NOT x?`);
  });
  it("identifier with spaces is quoted", () => {
    expect(encodeText({ type: "EQUALS", field: "my field", value: "v" })).toBe(`"my field" = "v"`);
  });
});

function ok(s: string) {
  const r = parseText(s);
  if (!r.ok) throw new Error(`parse failed at ${r.error.pos}: ${r.error.message}`);
  return r.value;
}

describe("parseText (parity with Go parser)", () => {
  it("bare word → SEARCH", () => {
    expect(ok("hello")).toEqual({ type: "SEARCH", field: "", value: "hello" });
  });
  it("key = value", () => {
    expect(ok("host = srv-1")).toEqual({ type: "EQUALS", field: "host", value: "srv-1" });
  });
  it("AND", () => {
    expect(ok("a=1 AND b=2")).toEqual({
      type: "AND",
      args: [
        { type: "EQUALS", field: "a", value: "1" },
        { type: "EQUALS", field: "b", value: "2" },
      ],
    });
  });
  it("& symbol == AND", () => {
    expect(ok("a=1 & b=2")).toEqual(ok("a=1 AND b=2"));
  });
  it("implicit AND on adjacency", () => {
    expect(ok("a=1 b=2")).toEqual(ok("a=1 AND b=2"));
  });
  it("OR", () => {
    expect(ok("a=1 OR b=2")).toEqual({
      type: "OR",
      args: [
        { type: "EQUALS", field: "a", value: "1" },
        { type: "EQUALS", field: "b", value: "2" },
      ],
    });
  });
  it("NOT and !", () => {
    expect(ok("NOT a=1")).toEqual({
      type: "NOT",
      arg: { type: "EQUALS", field: "a", value: "1" },
    });
    expect(ok("!a=1")).toEqual(ok("NOT a=1"));
  });
  it("parentheses change precedence", () => {
    expect(ok("NOT (a=1 AND b=2)")).toEqual({
      type: "NOT",
      arg: {
        type: "AND",
        args: [
          { type: "EQUALS", field: "a", value: "1" },
          { type: "EQUALS", field: "b", value: "2" },
        ],
      },
    });
  });
  it("precedence: NOT > AND > OR", () => {
    expect(ok("NOT a=1 AND b=2")).toEqual({
      type: "AND",
      args: [
        { type: "NOT", arg: { type: "EQUALS", field: "a", value: "1" } },
        { type: "EQUALS", field: "b", value: "2" },
      ],
    });
  });
  it("EXISTS keyword + ? shortcut", () => {
    expect(ok("trace_id EXISTS")).toEqual({ type: "EXISTS", field: "trace_id" });
    expect(ok("trace_id?")).toEqual({ type: "EXISTS", field: "trace_id" });
  });
  it("MATCHES keyword + ~ shortcut", () => {
    expect(ok(`msg MATCHES "[aA]"`)).toEqual({ type: "MATCHES", field: "msg", value: "[aA]" });
    expect(ok(`msg ~ "[aA]"`)).toEqual(ok(`msg MATCHES "[aA]"`));
  });
  it("comparison ops", () => {
    expect(ok("n > 5")).toEqual({ type: "GT", field: "n", value: 5 });
    expect(ok("n >= 5")).toEqual({ type: "GE", field: "n", value: 5 });
    expect(ok("n < 5")).toEqual({ type: "LT", field: "n", value: 5 });
    expect(ok("n <= 5")).toEqual({ type: "LE", field: "n", value: 5 });
  });
  it("CONTAINS keyword", () => {
    expect(ok(`tags CONTAINS "prod"`)).toEqual({
      type: "CONTAINS",
      field: "tags",
      value: "prod",
    });
  });
  it("IN keyword with array", () => {
    expect(ok(`sev IN ["err", "crit"]`)).toEqual({
      type: "IN",
      field: "sev",
      value: ["err", "crit"],
    });
  });
  it("quoted identifier with spaces", () => {
    expect(ok(`"my field" = v`)).toEqual({ type: "EQUALS", field: "my field", value: "v" });
  });
  it("empty input → ALWAYS_TRUE", () => {
    expect(ok("")).toEqual({ type: "ALWAYS_TRUE" });
    expect(ok("   ")).toEqual({ type: "ALWAYS_TRUE" });
  });
  it("error carries position", () => {
    const r = parseText("a =");
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.error.pos).toBeGreaterThanOrEqual(2);
  });
});

describe("encode/parse round trip", () => {
  const cases: Condition[] = [
    { type: "ALWAYS_TRUE" },
    { type: "EQUALS", field: "a", value: "1" },
    { type: "MATCHES", field: "m", value: "^foo$" },
    { type: "EXISTS", field: "x" },
    { type: "IN", field: "k", value: ["a", "b"] },
    { type: "LT", field: "n", value: 3 },
    {
      type: "AND",
      args: [
        { type: "EQUALS", field: "a", value: "1" },
        {
          type: "OR",
          args: [
            { type: "EQUALS", field: "b", value: "2" },
            { type: "EQUALS", field: "c", value: "3" },
          ],
        },
      ],
    },
    { type: "NOT", arg: { type: "EXISTS", field: "y" } },
  ];
  it.each(cases)("round-trips %j", (c) => {
    const r = parseText(encodeText(c));
    expect(r.ok).toBe(true);
    if (r.ok) expect(r.value).toEqual(c);
  });
});
