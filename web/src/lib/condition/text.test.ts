// web/src/lib/condition/text.test.ts
import { describe, it, expect } from "vitest";
import { encodeText } from "./text";
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
