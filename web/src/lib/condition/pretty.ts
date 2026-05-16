// Pretty-print a Condition AST as an infix string suitable for a
// single-line table cell. Falls back to the canonical text form for
// anything other than the common shapes.
//
// Examples:
//   {type:"EQUALS", field:"host", value:"x"}      → `host = "x"`
//   {type:"AND", args:[…,…]}                       → `(a AND b)`
//   {type:"NOT", arg:{…}}                          → `NOT (…)`
//   {type:"MATCHES", field:"host", value:"^prod"} → `host matches "^prod"`

import type { Condition, GroupOp } from "./types";
import { encodeText } from "./text";

const BINARY_OP_LABELS: Record<string, string> = {
  EQUALS: "=",
  "=": "=",
  NOT_EQUALS: "≠",
  "!=": "≠",
  LT: "<",
  "<": "<",
  LE: "≤",
  "<=": "≤",
  GT: ">",
  ">": ">",
  GE: "≥",
  ">=": "≥",
  MATCHES: "matches",
  CONTAINS: "contains",
};

function isGroup(c: Condition): c is { type: GroupOp; args: Condition[] } {
  return c.type === "AND" || c.type === "OR";
}

function quote(v: unknown): string {
  if (typeof v === "string") return JSON.stringify(v);
  return String((v as string | number | boolean | null | undefined) ?? "");
}

export function prettyCondition(c: Condition | undefined | null): string {
  if (!c || c.type === "ALWAYS_TRUE") return "always";
  if (c.type === "NOT") {
    return `NOT ${prettyCondition(c.arg)}`;
  }
  if (isGroup(c)) {
    if (!c.args || c.args.length === 0) return "always";
    if (c.args.length === 1) return prettyCondition(c.args[0]);
    const joiner = ` ${c.type} `;
    return "(" + c.args.map(prettyCondition).join(joiner) + ")";
  }
  if (c.type === "EXISTS") {
    return `${(c as { field: string }).field}?`;
  }
  if (c.type === "IN") {
    const cond = c as { field: string; value: unknown };
    return `${cond.field} in ${JSON.stringify(cond.value)}`;
  }
  if (c.type === "SEARCH") {
    return `search ${quote((c as { value: unknown }).value)}`;
  }
  const op = BINARY_OP_LABELS[c.type];
  if (op) {
    const cond = c as { field: string; value: unknown };
    return `${cond.field} ${op} ${quote(cond.value)}`;
  }
  // Unknown shape — fall back to the canonical text encoder.
  try {
    return encodeText(c);
  } catch {
    return "(invalid)";
  }
}
