import type { Condition, ConditionPath, GroupOp } from "./types";

export function emptyCondition(): Condition {
  return { type: "ALWAYS_TRUE" };
}

export function wrapInGroup(cond: Condition, type: GroupOp = "AND"): Condition {
  return { type, args: [cond] };
}

function isGroup(c: Condition): c is { type: GroupOp; args: Condition[] } {
  return c.type === "AND" || c.type === "OR";
}

export function replaceAtPath(root: Condition, path: ConditionPath, next: Condition): Condition {
  if (path.length === 0) return next;
  if (!isGroup(root)) return root;
  const [head, ...rest] = path;
  if (head === undefined || head < 0 || head >= root.args.length) return root;
  const args = root.args.slice();
  args[head] = replaceAtPath(root.args[head]!, rest, next);
  return { ...root, args };
}

export function removeAtPath(root: Condition, path: ConditionPath): Condition {
  if (path.length === 0) return emptyCondition();
  if (!isGroup(root)) return root;
  const [head, ...rest] = path;
  if (head === undefined || head < 0 || head >= root.args.length) return root;
  if (rest.length === 0) {
    return { ...root, args: root.args.filter((_, i) => i !== head) };
  }
  const args = root.args.slice();
  args[head] = removeAtPath(root.args[head]!, rest);
  return { ...root, args };
}

export function insertChildAtEnd(
  root: Condition,
  path: ConditionPath,
  child: Condition,
): Condition {
  if (path.length === 0) {
    if (!isGroup(root)) {
      return { type: "AND", args: [root, child] };
    }
    return { ...root, args: [...root.args, child] };
  }
  if (!isGroup(root)) return root;
  const [head, ...rest] = path;
  if (head === undefined || head < 0 || head >= root.args.length) return root;
  const args = root.args.slice();
  args[head] = insertChildAtEnd(root.args[head]!, rest, child);
  return { ...root, args };
}
