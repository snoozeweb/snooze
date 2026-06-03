import { describe, expect, it } from "vitest";
import { tokenize } from "./lexer";

describe("searchdsl tokenize", () => {
  it("emits an EOF for empty input", () => {
    const toks = tokenize("");
    expect(toks).toHaveLength(1);
    expect(toks[0]?.kind).toBe("eof");
  });

  it("tokenizes a key-value comparison", () => {
    const toks = tokenize("host = myhost01");
    const kinds = toks.map((t) => t.kind);
    expect(kinds).toEqual(["ident", "eq", "ident", "eof"]);
    expect(toks[0]?.text).toBe("host");
    expect(toks[2]?.text).toBe("myhost01");
  });

  it("recognises operators by symbol and keyword", () => {
    const toks = tokenize("a=1 b!=2 c<3 d<=4 e>5 f>=6 g~x h?");
    const ops = toks.filter((t) =>
      ["eq", "neq", "lt", "lte", "gt", "gte", "matches", "exists_sym"].includes(t.kind),
    );
    expect(ops.map((t) => t.kind)).toEqual([
      "eq",
      "neq",
      "lt",
      "lte",
      "gt",
      "gte",
      "matches",
      "exists_sym",
    ]);
  });

  it("recognises AND / OR / NOT keywords case-insensitively", () => {
    const toks = tokenize("a And b oR c NoT d");
    const kinds = toks.map((t) => t.kind);
    expect(kinds).toContain("and");
    expect(kinds).toContain("or");
    expect(kinds).toContain("not");
  });

  it("recognises AND / OR / NOT symbol forms", () => {
    const toks = tokenize("a & b | !c");
    const kinds = toks.map((t) => t.kind);
    expect(kinds).toEqual(["ident", "and", "ident", "or", "not", "ident", "eof"]);
  });

  it("handles double-quoted strings with escapes", () => {
    const toks = tokenize(`field = "value with \\"quote\\" and \\n"`);
    const str = toks.find((t) => t.kind === "string");
    expect(str?.value).toBe('value with "quote" and \n');
  });

  it("handles single-quoted strings", () => {
    const toks = tokenize(`'my field' = 'my value'`);
    const strs = toks.filter((t) => t.kind === "string");
    expect(strs.map((s) => s.value)).toEqual(["my field", "my value"]);
  });

  it("flags an unterminated string as an error token", () => {
    const toks = tokenize(`field = "unterminated`);
    expect(toks.some((t) => t.kind === "error")).toBe(true);
  });

  it("tokenizes integers, negative ints, and floats", () => {
    const toks = tokenize("a=123 b=-42 c=3.14");
    const nums = toks.filter((t) => t.kind === "number").map((t) => t.text);
    expect(nums).toEqual(["123", "-42", "3.14"]);
  });

  it("recognises true/false as bool tokens", () => {
    const toks = tokenize("mybool=TRUE other=false");
    const bools = toks.filter((t) => t.kind === "bool").map((t) => t.text.toLowerCase());
    expect(bools).toEqual(["true", "false"]);
  });

  it("tokenizes arrays and dicts", () => {
    const toks = tokenize("a=[1,2,3] b={k: 1, v: 'x'}");
    const kinds = toks.map((t) => t.kind);
    expect(kinds).toContain("lbrack");
    expect(kinds).toContain("rbrack");
    expect(kinds).toContain("lbrace");
    expect(kinds).toContain("rbrace");
    expect(kinds).toContain("colon");
  });

  it("identifier may contain dots, dashes, underscores", () => {
    const toks = tokenize("my-field.sub_path = ok");
    expect(toks[0]?.kind).toBe("ident");
    expect(toks[0]?.text).toBe("my-field.sub_path");
  });

  it("records absolute byte positions", () => {
    const src = "host = foo AND severity = warning";
    const toks = tokenize(src);
    for (const t of toks) {
      if (t.kind === "eof") continue;
      expect(src.slice(t.pos, t.pos + t.len)).toBe(t.text);
    }
  });
});
