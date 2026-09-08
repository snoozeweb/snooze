import { describe, expect, it } from "vitest";
import { prettyCondition } from "./pretty";

describe("prettyCondition", () => {
  it("renders the empty / always-true shape as 'always'", () => {
    expect(prettyCondition({ type: "ALWAYS_TRUE" })).toBe("always");
    expect(prettyCondition(undefined)).toBe("always");
  });
  it("renders binary ops with quoted strings", () => {
    expect(prettyCondition({ type: "EQUALS", field: "host", value: "x" })).toBe(`host = "x"`);
    expect(prettyCondition({ type: "MATCHES", field: "host", value: "^p" })).toBe(
      `host matches "^p"`,
    );
    expect(prettyCondition({ type: "GT", field: "retries", value: 3 } as never)).toBe(
      `retries > 3`,
    );
  });
  it("renders AND / OR groups infix", () => {
    expect(
      prettyCondition({
        type: "AND",
        args: [
          { type: "EQUALS", field: "host", value: "a" },
          { type: "EQUALS", field: "severity", value: "critical" },
        ],
      }),
    ).toBe(`(host = "a" AND severity = "critical")`);
  });
  it("renders NOT prefix", () => {
    expect(prettyCondition({ type: "NOT", arg: { type: "EXISTS", field: "shelved" } })).toBe(
      "NOT shelved?",
    );
  });
  it("collapses a single-arg group to its child", () => {
    expect(
      prettyCondition({ type: "AND", args: [{ type: "EQUALS", field: "a", value: 1 } as never] }),
    ).toBe(`a = 1`);
  });
  it("prints a boolean operand unquoted", () => {
    // `batch = "true"` would describe a condition that matches nothing — the
    // drivers compare booleans by type.
    expect(prettyCondition({ type: "EQUALS", field: "batch", value: true })).toBe("batch = true");
    expect(prettyCondition({ type: "NOT_EQUALS", field: "batch", value: false })).toBe(
      "batch ≠ false",
    );
  });
});
