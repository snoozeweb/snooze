// web/src/lib/condition/text.ts
import type { Condition, ConditionType } from "./types";

export type TextParseError = { message: string; pos: number };
export type TextParseResult = { ok: true; value: Condition } | { ok: false; error: TextParseError };

const IDENT_RE = /^[A-Za-z_][A-Za-z0-9_.-]*$/;

function quoteString(s: string): string {
  return `"${s.replace(/\\/g, "\\\\").replace(/"/g, '\\"').replace(/\n/g, "\\n").replace(/\t/g, "\\t")}"`;
}

function encodeIdent(s: string): string {
  return IDENT_RE.test(s) ? s : quoteString(s);
}

function encodeValue(v: string | number | boolean): string {
  if (typeof v === "number") return Number.isInteger(v) ? v.toString() : v.toString();
  if (typeof v === "boolean") return v ? "true" : "false";
  return quoteString(v);
}

function encodeArray(v: string[]): string {
  return `[${v.map(encodeValue).join(", ")}]`;
}

const LEAF_OP: Partial<Record<ConditionType, string>> = {
  EQUALS: "=",
  MATCHES: "~",
  CONTAINS: "CONTAINS",
  LT: "<",
  LE: "<=",
  GT: ">",
  GE: ">=",
};

const LOGICAL_OPS = new Set<ConditionType>(["AND", "OR", "NOT"]);

function encodeChild(child: Condition, parent: ConditionType): string {
  const out = encodeText(child);
  // Always wrap logical-op children inside another logical op for unambiguous output.
  // Leaf nodes (EXISTS, EQUALS, etc.) are already atomic and never need parens.
  if (LOGICAL_OPS.has(child.type) && LOGICAL_OPS.has(parent)) return `(${out})`;
  return out;
}

export function encodeText(c: Condition): string {
  switch (c.type) {
    case "ALWAYS_TRUE":
      return "";
    case "SEARCH":
      return quoteString(c.value);
    case "EXISTS":
      return `${encodeIdent(c.field)}?`;
    case "IN":
      return `${encodeIdent(c.field)} IN ${encodeArray(c.value)}`;
    case "EQUALS":
    case "MATCHES":
    case "CONTAINS":
    case "LT":
    case "LE":
    case "GT":
    case "GE": {
      const op = LEAF_OP[c.type]!;
      return `${encodeIdent(c.field)} ${op} ${encodeValue(c.value)}`;
    }
    case "NOT":
      return `NOT ${encodeChild(c.arg, "NOT")}`;
    case "AND":
      return c.args.map((a) => encodeChild(a, "AND")).join(" AND ");
    case "OR":
      return c.args.map((a) => encodeChild(a, "OR")).join(" OR ");
  }
}

// Parser is filled in by Task 2. Stub keeps the import surface complete.
export function parseText(_s: string): TextParseResult {
  return { ok: false, error: { message: "parseText not yet implemented", pos: 0 } };
}
