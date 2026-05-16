import type { Rule } from "./types";

export const ROOT = "__root__";

export type TreeNode = {
  rule: Rule;
  depth: number;
  children: TreeNode[];
};

export function parentKey(r: Rule): string {
  return r.parents?.[0] ?? ROOT;
}

export function sortSiblings(rules: Rule[]): Rule[] {
  return [...rules].sort((a, b) => {
    const ao = a.tree_order ?? Number.MAX_SAFE_INTEGER;
    const bo = b.tree_order ?? Number.MAX_SAFE_INTEGER;
    if (ao !== bo) return ao - bo;
    return (a.name ?? "").localeCompare(b.name ?? "");
  });
}

export function buildTree(rules: Rule[]): { roots: TreeNode[]; byParent: Map<string, Rule[]> } {
  const uids = new Set(rules.map((r) => r.uid).filter((u): u is string => !!u));
  const byParent = new Map<string, Rule[]>();
  for (const r of rules) {
    // A rule's referenced parent might not exist in the loaded set
    // (deleted, or filtered out). Treat orphans as top-level so they
    // remain reachable instead of disappearing.
    const p = r.parents?.[0];
    const key = p && uids.has(p) ? p : ROOT;
    const arr = byParent.get(key) ?? [];
    arr.push(r);
    byParent.set(key, arr);
  }
  function build(parent: string, depth: number): TreeNode[] {
    return sortSiblings(byParent.get(parent) ?? []).map((rule) => ({
      rule,
      depth,
      children: rule.uid ? build(rule.uid, depth + 1) : [],
    }));
  }
  return { roots: build(ROOT, 0), byParent };
}
