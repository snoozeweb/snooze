import type { Condition } from "@/lib/condition/types";

function childArgs(c: Condition): Condition[] {
  if (c.type === "NOT") return [c.arg];
  if (c.type === "AND" || c.type === "OR") return c.args;
  return [];
}

/**
 * switchLogicOp converts a logic node (AND/OR/NOT) to a different operator.
 *
 * The important case is switching a multi-child AND/OR to NOT: NOT is unary, so
 * the old behavior kept only the first child and silently discarded the rest.
 * Instead we negate the whole group — NOT(AND(a,b,c)) — so no sub-condition is
 * ever lost when a user flips a group to NOT.
 */
export function switchLogicOp(
  current: Condition,
  nextOp: "AND" | "OR" | "NOT",
  makeLeaf: () => Condition,
): Condition {
  const children = childArgs(current);
  if (nextOp === "NOT") {
    if (children.length <= 1) {
      return { type: "NOT", arg: children[0] ?? makeLeaf() };
    }
    const groupOp: "AND" | "OR" = current.type === "OR" ? "OR" : "AND";
    return { type: "NOT", arg: { type: groupOp, args: children } };
  }
  // Switching to AND/OR. If we're leaving a NOT that wraps a group (the shape
  // the NOT branch above produces), unwrap to that group's own args so the
  // round-trip restores the original — otherwise we'd nest a group inside a
  // group and pad it with a spurious blank leaf.
  const inner = current.type === "NOT" ? current.arg : undefined;
  const base =
    inner && (inner.type === "AND" || inner.type === "OR") ? inner.args : children;
  // AND/OR need at least two args so the editor stays paired.
  let args = base.slice();
  if (args.length < 2) args = [...args, makeLeaf()];
  return { type: nextOp, args };
}
