// RulesTreeTable renders rules as a tree (parents[]/tree_order) with
// drag-and-drop sibling reorder. Cross-parent moves are not supported in
// this iteration — the user can change a rule's parent via its editor form
// or backend API.
//
// Data flow:
//   1. RulesPage fetches all rules in one page (limit=1000) and hands them
//      to this component, so we have the full tree client-side.
//   2. buildTree groups by parents[0], sorts each level by tree_order ASC.
//   3. <DndContext> wraps the whole tree; one <SortableContext> per sibling
//      group keeps drag operations confined to a single level.
//   4. On drop, we recompute tree_order across the affected sibling group
//      and issue a PATCH per rule whose tree_order actually changed.
import { useCallback, useMemo, useState } from "react";
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  arrayMove,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Badge } from "@/shared/ui/Badge";
import { Code } from "@/shared/ui/Code";
import { Icon } from "@/shared/icons/Icon";
import { RowDetailPanel } from "@/shared/ui/RowDetailPanel";
import { toast } from "@/shared/ui/toast/useToast";
import { prettyCondition } from "@/lib/condition/pretty";
import { Rules } from "./api";
import type { Rule } from "./types";
import styles from "./RulesTreeTable.module.css";

// __ROOT__ is the synthetic parent id used internally for top-level rules
// (rules with no parents[]). It never travels to the wire.
const ROOT = "__root__";

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

export type RulesTreeTableProps = {
  rules: Rule[];
  onRowOpen: (r: Rule) => void;
};

export function RulesTreeTable({ rules, onRowOpen }: RulesTreeTableProps) {
  const update = Rules.useUpdate();
  const { roots, byParent } = useMemo(() => buildTree(rules), [rules]);

  // Per-row "details" expansion (JSON + audit) — kept separate from the
  // tree's natural parent/child rendering (which is always-expanded today).
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set<string>());
  const toggleExpanded = useCallback((key: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }, []);

  // The PointerSensor activates only after a 6px drag so plain row clicks
  // (to open the editor drawer) still work. The KeyboardSensor lets users
  // reorder via keyboard for a11y.
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  function handleDragEnd(e: DragEndEvent) {
    const activeId = e.active.id as string;
    const overId = (e.over?.id as string | undefined) ?? undefined;
    if (!overId || activeId === overId) return;
    const active = rules.find((r) => r.uid === activeId);
    const over = rules.find((r) => r.uid === overId);
    if (!active || !over) return;
    const activeParent = parentKey(active);
    const overParent = parentKey(over);
    if (activeParent !== overParent) {
      // Cross-parent moves are not allowed in this iteration. Surface a
      // hint instead of silently doing nothing.
      toast.info("Drop on a sibling to reorder. Cross-parent moves come from the rule editor.");
      return;
    }
    const siblings = sortSiblings(byParent.get(activeParent) ?? []);
    const oldIdx = siblings.findIndex((r) => r.uid === activeId);
    const newIdx = siblings.findIndex((r) => r.uid === overId);
    if (oldIdx < 0 || newIdx < 0) return;
    const reordered = arrayMove(siblings, oldIdx, newIdx);
    const changes = reordered
      .map((r, i) => ({ r, i }))
      .filter(({ r, i }) => r.tree_order !== i);
    // Fire PATCH for every sibling whose ordinal shifted. We don't await
    // each one serially — the queries layer batches list invalidation.
    for (const { r, i } of changes) {
      if (!r.uid) continue;
      update.mutate(
        { uid: r.uid, body: { tree_order: i } as Partial<Rule> },
        {
          onError: (err) => {
            toast.error(`Failed to reorder ${r.name}: ${err.detail}`);
          },
        },
      );
    }
  }

  if (rules.length === 0) {
    return (
      <div className={styles.tree}>
        <div className={styles.empty}>No rules yet.</div>
      </div>
    );
  }

  return (
    <div className={styles.tree}>
      <div className={styles.headerRow}>
        <span />
        <span />
        <span>Name</span>
        <span>Condition</span>
        <span>Modifications</span>
      </div>
      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
        <SiblingGroup
          nodes={roots}
          onRowOpen={onRowOpen}
          expanded={expanded}
          toggleExpanded={toggleExpanded}
        />
      </DndContext>
    </div>
  );
}

type SiblingGroupProps = {
  nodes: TreeNode[];
  onRowOpen: (r: Rule) => void;
  expanded: Set<string>;
  toggleExpanded: (key: string) => void;
};

// SiblingGroup renders one level of the tree inside a SortableContext.
// Each row recursively renders its own children inside their own
// SortableContext, which means reorder operations stay scoped to a single
// parent. The DndContext at the top level catches the drag-end and uses
// the parents[] of the active/over rules to identify the affected group.
function SiblingGroup({ nodes, onRowOpen, expanded, toggleExpanded }: SiblingGroupProps) {
  const ids = nodes
    .map((n) => n.rule.uid)
    .filter((u): u is string => !!u);
  return (
    <SortableContext items={ids} strategy={verticalListSortingStrategy}>
      <div className={styles.childList}>
        {nodes.map((n) => (
          <SortableTreeRow
            key={n.rule.uid ?? n.rule.name}
            node={n}
            onRowOpen={onRowOpen}
            expanded={expanded}
            toggleExpanded={toggleExpanded}
          />
        ))}
      </div>
    </SortableContext>
  );
}

type SortableTreeRowProps = {
  node: TreeNode;
  onRowOpen: (r: Rule) => void;
  expanded: Set<string>;
  toggleExpanded: (key: string) => void;
};

function SortableTreeRow({ node, onRowOpen, expanded, toggleExpanded }: SortableTreeRowProps) {
  const id = node.rule.uid ?? node.rule.name;
  const sortable = useSortable({ id });
  const { attributes, listeners, setNodeRef, transform, transition, isDragging, isOver } = sortable;
  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition: transition ?? undefined,
  };
  // Depth-based indent: each level adds a fixed margin. We use a
  // leading "twig" character (└─ for inner, ├─ for last) so the
  // tree shape is legible even without subtle background banding.
  const indentPx = node.depth * 16;
  const enabled = node.rule.enabled !== false;
  const mods = node.rule.modifications ?? [];
  const rowKey = node.rule.uid ?? node.rule.name;
  const isExpanded = expanded.has(rowKey);
  return (
    <div>
      <div
        ref={setNodeRef}
        style={style}
        className={[
          styles.row,
          isDragging ? styles.rowDragging : "",
          isOver ? styles.rowOver : "",
        ]
          .filter(Boolean)
          .join(" ")}
        {...(!enabled ? { "data-disabled": "true" } : {})}
        onClick={(e) => {
          // Don't open the drawer when the click started on the drag handle
          // or the per-row details chevron.
          const target = e.target as HTMLElement;
          if (target.closest("[data-drag-handle]")) return;
          if (target.closest("[data-expand-toggle]")) return;
          onRowOpen(node.rule);
        }}
        role="row"
      >
        <button
          type="button"
          data-expand-toggle
          className={styles.expandBtn}
          aria-label={`Expand row ${node.rule.name}`}
          aria-expanded={isExpanded}
          onClick={(e) => {
            // Prevent the row's onClick (which opens the editor) from firing.
            e.stopPropagation();
            toggleExpanded(rowKey);
          }}
        >
          <Icon name={isExpanded ? "chevron-down" : "chevron-right"} size={14} />
        </button>
        <span
          {...attributes}
          {...listeners}
          data-drag-handle
          className={styles.handle}
          aria-label={`Drag ${node.rule.name}`}
        >
          <Icon name="grip" size={16} />
        </span>
        <span className={styles.nameCell}>
          {node.depth > 0 ? (
            <span className={styles.indent} style={{ width: indentPx }}>
              <span className={styles.twig}>└─ </span>
            </span>
          ) : null}
          <Code>{node.rule.name}</Code>
        </span>
        <span style={{ fontFamily: "var(--font-mono)", fontSize: "var(--text-xs)" }}>
          {prettyCondition(node.rule.condition)}
        </span>
        <span style={{ display: "inline-flex", gap: "var(--space-1)", flexWrap: "wrap" }}>
          {mods.length === 0 ? (
            <span className={styles.comment}>—</span>
          ) : (
            mods.map((m, i) => (
              <Badge key={i} variant="neutral">
                {String(m[0] ?? "")} {String(m[1] ?? "")}
              </Badge>
            ))
          )}
        </span>
      </div>
      {isExpanded ? (
        <div className={styles.expandedRow}>
          <RowDetailPanel
            row={node.rule as unknown as Record<string, unknown>}
            objectType="rule"
            objectId={node.rule.uid}
          />
        </div>
      ) : null}
      {node.children.length > 0 ? (
        <SiblingGroup
          nodes={node.children}
          onRowOpen={onRowOpen}
          expanded={expanded}
          toggleExpanded={toggleExpanded}
        />
      ) : null}
    </div>
  );
}
