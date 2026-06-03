import type { Rule } from "./types";

export const ROOT = "__root__";

export type TreeNode = {
  rule: Rule;
  depth: number;
  children: TreeNode[];
};

export type FlatNode = {
  rule: Rule;
  depth: number;
  /** Parent uid in the tree, or `ROOT`. */
  parentId: string;
  /** Direct child uids in order, for collapse/drag-subtree logic. */
  childIds: string[];
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

/** Flattens a tree into a depth-aware ordered list. Used by the drag-and-drop
 *  surface so the entire tree shares a single SortableContext (which is what
 *  enables cross-parent and parent-as-child moves). */
export function flattenTree(roots: TreeNode[]): FlatNode[] {
  const out: FlatNode[] = [];
  function walk(node: TreeNode, parentId: string) {
    out.push({
      rule: node.rule,
      depth: node.depth,
      parentId,
      childIds: node.children.map((c) => c.rule.uid ?? c.rule.name),
    });
    for (const c of node.children) walk(c, node.rule.uid ?? node.rule.name);
  }
  for (const r of roots) walk(r, ROOT);
  return out;
}

/** Returns the set of uids belonging to `id` and all its descendants. */
export function collectSubtreeIds(items: FlatNode[], id: string): Set<string> {
  const out = new Set<string>([id]);
  const idx = items.findIndex((it) => (it.rule.uid ?? it.rule.name) === id);
  if (idx < 0) return out;
  const base = items[idx]!.depth;
  for (let i = idx + 1; i < items.length; i++) {
    if (items[i]!.depth <= base) break;
    out.add(items[i]!.rule.uid ?? items[i]!.rule.name);
  }
  return out;
}

/** Decide where a row should land when dragged.
 *
 *  Given the flat list with the active item *removed*, the index it would
 *  occupy after drop, and the horizontal offset (px) the user has dragged,
 *  returns the target parent uid + depth.
 *
 *  Behaviour mirrors the file-tree pattern: snap to the previous row's depth
 *  by default, but allow indenting one level deeper (becomes a child of the
 *  row above) if the user drags far enough to the right, and outdenting all
 *  the way back to root if the user drags to the left. */
export function projectDrop(
  rest: FlatNode[],
  overIndex: number,
  offsetX: number,
  indentPx = 20,
): { parentId: string; depth: number } {
  const prev = overIndex > 0 ? rest[overIndex - 1] : undefined;
  const next = rest[overIndex];
  if (!prev && !next) return { parentId: ROOT, depth: 0 };

  const dragDepthDelta = Math.round(offsetX / indentPx);
  const projected = (prev ? prev.depth : 0) + dragDepthDelta;
  // Allowed range: at most prev.depth + 1, at least next.depth (so we don't
  // tear the next row away from its own parent), and not less than 0.
  const maxDepth = prev ? prev.depth + 1 : 0;
  const minDepth = next ? next.depth : 0;
  const depth = Math.max(minDepth, Math.min(maxDepth, projected));

  // Figure out which parent the row at this depth belongs to.
  let parentId: string;
  if (depth === 0 || !prev) {
    parentId = ROOT;
  } else if (depth === prev.depth + 1) {
    // One level deeper than the previous row → child of the previous row.
    parentId = prev.rule.uid ?? prev.rule.name;
  } else if (depth === prev.depth) {
    // Same level as the previous row → shares its parent.
    parentId = prev.parentId;
  } else {
    // Shallower than the previous row → walk up the chain to find the
    // ancestor at the target depth.
    let cursor: FlatNode | undefined = prev;
    while (cursor && cursor.depth >= depth) {
      const parent = rest.find((n) => (n.rule.uid ?? n.rule.name) === cursor!.parentId);
      cursor = parent;
    }
    parentId = cursor ? (cursor.rule.uid ?? cursor.rule.name) : ROOT;
  }
  return { parentId, depth };
}
