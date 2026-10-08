import type { Condition } from "@/lib/condition/types";

function childArgs(c: Condition): Condition[] {
  if (c.type === "NOT") return [c.arg];
  if (c.type === "AND" || c.type === "OR") return c.args;
  return [];
}

/**
 * switchLogicOp converts a logic node (AND/OR/NOT) to a different operator.
 *
 * Switching a multi-child AND/OR to NOT: NOT is unary, so only the first child
 * is kept (negated) and the remaining children are dropped — no extra parent
 * group is created.
 */
export function switchLogicOp(
  current: Condition,
  nextOp: "AND" | "OR" | "NOT",
  makeLeaf: () => Condition,
): Condition {
  const children = childArgs(current);
  if (nextOp === "NOT") {
    return { type: "NOT", arg: children[0] ?? makeLeaf() };
  }
  // Switching to AND/OR. If we're leaving a NOT that wraps a group, unwrap to
  // that group's own args rather than nesting a group inside a group.
  const inner = current.type === "NOT" ? current.arg : undefined;
  const base = inner && (inner.type === "AND" || inner.type === "OR") ? inner.args : children;
  // AND/OR need at least two args so the editor stays paired.
  let args = base.slice();
  if (args.length < 2) args = [...args, makeLeaf()];
  return { type: nextOp, args };
}
