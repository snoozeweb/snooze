// RulesTreeTable renders rules as a tree (parents[]/tree_order) with full
// drag-and-drop reordering, including:
//
// - cross-parent moves (drop a child under a different parent),
// - parent-as-child moves (drop a top-level rule onto another rule to
//   nest it),
// - subtree dragging (dragging a parent moves all its descendants).
//
// The single <DndContext> + flat <SortableContext> pattern is what enables
// these moves: a per-sibling-group SortableContext setup fundamentally
// can't represent a drop across levels.
//
// Visual model: the dragged subtree disappears from the list while the
// gesture is in flight, and a floating preview (<DragOverlay>) follows the
// cursor. A horizontal accent-coloured indicator line is rendered at the
// projected drop slot, indented to the projected depth, so the user can
// see exactly where (and at what nesting level) the rule will land.
//
// Data flow:
//   1. RulesPage fetches every rule and hands them down. We mirror that
//      into local state so a drop can apply optimistically — without it,
//      the row visibly snaps back to its origin before the PATCH lands
//      and react-query re-fetches.
//   2. buildTree groups by parents[0], sorts each level by tree_order.
//   3. flattenTree gives the visible list. While dragging a parent, the
//      whole subtree is hidden — it rides with the preview.
//   4. On drop, we compute the new flat order + parent assignment, fire a
//      PATCH per rule whose (parents, tree_order) changed, and apply the
//      new order locally. The next refetch reconciles.
import { Fragment, useCallback, useEffect, useMemo, useState } from "react";
import {
  DndContext,
  DragOverlay,
  KeyboardSensor,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragMoveEvent,
  type DragStartEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { Checkbox } from "@/shared/ui/Checkbox";
import { Code } from "@/shared/ui/Code";
import { Icon } from "@/shared/icons/Icon";
import { RowDetailPanel } from "@/shared/ui/RowDetailPanel";
import { toast } from "@/shared/ui/toast/useToast";
import { prettyCondition } from "@/lib/condition/pretty";
import {
  ConfirmDeleteDialog,
  useConfirmDelete,
} from "@/shared/ui/resourceContextMenu";
import { Rules } from "./api";
import type { Rule } from "./types";
import {
  ROOT,
  buildTree,
  collectSubtreeIds,
  flattenTree,
  projectDrop,
  type FlatNode,
  type TreeNode,
} from "./tree";
import styles from "./RulesTreeTable.module.css";

export type { TreeNode };

const INDENT_PX = 20;

// No-op strategy: keep rows physically in place during drag. Visual feedback
// is handled by the drop indicator line + the DragOverlay clone, not by
// transforming the surrounding rows — that approach (verticalListSortingStrategy)
// fights with our per-row depth changes and produces inconsistent drop
// animations when crossing parent boundaries.
const noopStrategy = () => null;

export type RulesTreeTableProps = {
  rules: Rule[];
  onRowOpen: (r: Rule) => void;
  /** Persistent toolbar slot — host's "New" button etc. Bulk-selection
   *  mode replaces this with the delete action, just like DataTable. */
  toolbar?: React.ReactNode;
  toolbarHeader?: React.ReactNode;
};

export function RulesTreeTable({ rules, onRowOpen, toolbar, toolbarHeader }: RulesTreeTableProps) {
  const update = Rules.useUpdate();
  const remove = Rules.useRemove();

  // Local mirror of the prop, so we can apply drops optimistically before
  // the network round-trip — otherwise dnd-kit animates the row back to its
  // origin and only the post-refetch render shows the new order, which
  // reads as a flicker.
  const [localRules, setLocalRules] = useState<Rule[]>(rules);
  useEffect(() => setLocalRules(rules), [rules]);

  const { roots } = useMemo(() => buildTree(localRules), [localRules]);
  const fullFlat = useMemo(() => flattenTree(roots), [roots]);

  // Selection state. Toggling a parent toggles its full subtree.
  const [selected, setSelected] = useState<Set<string>>(() => new Set());
  // Cull stale selections when the underlying set of rules changes.
  useEffect(() => {
    const alive = new Set(localRules.map((r) => r.uid ?? r.name));
    setSelected((prev) => {
      let changed = false;
      const next = new Set<string>();
      for (const id of prev) {
        if (alive.has(id)) next.add(id);
        else changed = true;
      }
      return changed ? next : prev;
    });
  }, [localRules]);

  const toggleSelection = useCallback(
    (id: string) => {
      setSelected((prev) => {
        const subtree = collectSubtreeIds(fullFlat, id);
        const next = new Set(prev);
        const allOn = [...subtree].every((s) => prev.has(s));
        if (allOn) for (const s of subtree) next.delete(s);
        else for (const s of subtree) next.add(s);
        return next;
      });
    },
    [fullFlat],
  );

  const allSelected =
    localRules.length > 0 && localRules.every((r) => selected.has(r.uid ?? r.name));
  const someSelected = localRules.some((r) => selected.has(r.uid ?? r.name));
  const toggleAll = useCallback(() => {
    if (allSelected) setSelected(new Set());
    else setSelected(new Set(localRules.map((r) => r.uid ?? r.name)));
  }, [allSelected, localRules]);

  const selectedRules = useMemo(
    () => localRules.filter((r) => selected.has(r.uid ?? r.name)),
    [localRules, selected],
  );

  const confirmDelete = useConfirmDelete<Rule>({
    onDelete: (uid) => remove.mutateAsync(uid),
    noun: "rule",
    onAfter: () => setSelected(new Set()),
  });

  // Per-row "details" expansion. We force-close while dragging — leaving
  // expanded panels in place produces a janky drag with rows constantly
  // resizing, and the original Vue UI hid details during drag too.
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
  const toggleExpanded = useCallback((key: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }, []);

  // Drag state. `activeId` is the row being dragged; `overId` is the row
  // dnd-kit's collision detection reports under the cursor. `offsetX`
  // tracks horizontal drift so we can project nesting depth.
  const [activeId, setActiveId] = useState<string | null>(null);
  const [overId, setOverId] = useState<string | null>(null);
  const [offsetX, setOffsetX] = useState(0);

  // The PointerSensor activates only after 6px of movement so plain row
  // clicks (which open the editor drawer) still work.
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  // The dragged row + all its descendants are hidden during the gesture —
  // the floating preview shows the whole subtree visually, and the
  // remaining list represents the candidate drop slots.
  const subtreeIds = useMemo(
    () => (activeId ? collectSubtreeIds(fullFlat, activeId) : new Set<string>()),
    [activeId, fullFlat],
  );
  const renderedFlat = useMemo(() => {
    if (!activeId) return fullFlat;
    return fullFlat.filter((n) => !subtreeIds.has(n.rule.uid ?? n.rule.name));
  }, [activeId, fullFlat, subtreeIds]);

  const activeRule = useMemo(() => {
    if (!activeId) return null;
    return fullFlat.find((n) => (n.rule.uid ?? n.rule.name) === activeId)?.rule ?? null;
  }, [activeId, fullFlat]);

  // Project the drop target — both the slot index in `renderedFlat` (where
  // the indicator line should render) and the (parentId, depth) the active
  // row will adopt.
  const projection = useMemo(() => {
    if (!activeId) return null;
    // `overId` is the row currently under the cursor. The drop slot is the
    // gap immediately above that row, i.e. its index in renderedFlat.
    const slotIndex = overId
      ? renderedFlat.findIndex((n) => (n.rule.uid ?? n.rule.name) === overId)
      : renderedFlat.length;
    const safeSlot = slotIndex < 0 ? renderedFlat.length : slotIndex;
    const { parentId, depth } = projectDrop(renderedFlat, safeSlot, offsetX, INDENT_PX);
    return { parentId, depth, slotIndex: safeSlot };
  }, [activeId, overId, renderedFlat, offsetX]);

  const handleDragStart = useCallback((e: DragStartEvent) => {
    setActiveId(e.active.id as string);
    setOverId(null);
    setOffsetX(0);
    // Force-close every expanded details panel — leaving them open while
    // rows are reshuffling produces the worst kind of layout thrash.
    setExpanded(new Set());
  }, []);

  const handleDragMove = useCallback((e: DragMoveEvent) => {
    setOffsetX(e.delta.x);
    setOverId((e.over?.id as string | undefined) ?? null);
  }, []);

  const handleDragCancel = useCallback(() => {
    setActiveId(null);
    setOverId(null);
    setOffsetX(0);
  }, []);

  const handleDragEnd = useCallback(
    (e: DragEndEvent) => {
      const draggedId = e.active.id as string;
      setActiveId(null);
      setOverId(null);
      setOffsetX(0);
      if (!projection) return;

      // Reconstruct the new flat order. The active row + its subtree should
      // land contiguously at the projected slot in fullFlat (slotIndex is
      // expressed against renderedFlat — but they are 1:1 outside the
      // subtree, so the slot translates directly into fullFlat once we strip
      // the moving rows).
      const subtree = collectSubtreeIds(fullFlat, draggedId);
      const subtreeNodes = fullFlat.filter((n) =>
        subtree.has(n.rule.uid ?? n.rule.name),
      );
      const remainder = fullFlat.filter(
        (n) => !subtree.has(n.rule.uid ?? n.rule.name),
      );

      // Refuse drops that would create a cycle (dragging a parent onto its
      // own descendant).
      if (subtree.has(projection.parentId) && projection.parentId !== ROOT) {
        toast.info("Can't drop a rule inside its own subtree.");
        return;
      }

      // The active row adopts the projected parent + depth; its descendants
      // shift their depth by the same delta but keep their relative shape.
      const activeNode = fullFlat.find(
        (n) => (n.rule.uid ?? n.rule.name) === draggedId,
      );
      if (!activeNode) return;
      const depthDelta = projection.depth - activeNode.depth;

      const movedNodes: FlatNode[] = subtreeNodes.map((n, i) => ({
        ...n,
        depth: n.depth + depthDelta,
        parentId: i === 0 ? projection.parentId : n.parentId,
      }));

      const insertAtSafe = Math.min(Math.max(projection.slotIndex, 0), remainder.length);

      const reordered: FlatNode[] = [
        ...remainder.slice(0, insertAtSafe),
        ...movedNodes,
        ...remainder.slice(insertAtSafe),
      ];

      // Compute new (parents, tree_order) per uid, grouped by their parent.
      const byParent = new Map<string, FlatNode[]>();
      for (const n of reordered) {
        const arr = byParent.get(n.parentId) ?? [];
        arr.push(n);
        byParent.set(n.parentId, arr);
      }
      const patches: { uid: string; parents: string[]; tree_order: number }[] = [];
      for (const [parent, siblings] of byParent.entries()) {
        siblings.forEach((s, i) => {
          if (!s.rule.uid) return;
          const newParents = parent === ROOT ? [] : [parent];
          const prevParents = s.rule.parents ?? [];
          const prevOrder = s.rule.tree_order ?? -1;
          const parentsChanged =
            newParents.length !== prevParents.length ||
            newParents.some((p, idx) => p !== prevParents[idx]);
          if (parentsChanged || prevOrder !== i) {
            patches.push({ uid: s.rule.uid, parents: newParents, tree_order: i });
          }
        });
      }

      if (patches.length === 0) return;

      // Apply locally so the rendered order matches the user's intent
      // immediately — react-query's refetch will reconcile with the server.
      setLocalRules((prev) => {
        const idx = new Map(prev.map((r) => [r.uid ?? r.name, r] as const));
        const next: Rule[] = [];
        for (const n of reordered) {
          const key = n.rule.uid ?? n.rule.name;
          const base = idx.get(key) ?? n.rule;
          const parents = n.parentId === ROOT ? undefined : [n.parentId];
          next.push({
            ...base,
            ...(parents === undefined ? { parents: [] } : { parents }),
          });
        }
        // Stamp the per-parent tree_order so the next buildTree sees the
        // optimistic order.
        const counters = new Map<string, number>();
        return next.map((r) => {
          const p = r.parents?.[0] ?? ROOT;
          const ord = counters.get(p) ?? 0;
          counters.set(p, ord + 1);
          return { ...r, tree_order: ord };
        });
      });

      for (const p of patches) {
        update.mutate(
          {
            uid: p.uid,
            body: {
              parents: p.parents,
              tree_order: p.tree_order,
            } as Partial<Rule>,
          },
          {
            onError: (err) => toast.error(`Reorder failed: ${err.detail}`),
          },
        );
      }
    },
    [fullFlat, projection, update],
  );

  const isRowSelected = (id: string) => selected.has(id);

  const hasSelection = selectedRules.length > 0;
  const renderToolbar = toolbar !== undefined || toolbarHeader !== undefined || hasSelection;

  return (
    <div className={styles.wrap}>
      {renderToolbar ? (
        <div
          className={hasSelection ? styles.toolbarSelected : styles.toolbar}
          role="region"
          aria-label={hasSelection ? "Bulk actions" : "Table toolbar"}
        >
          {hasSelection ? (
            <span className={styles.toolbarCount}>{selectedRules.length} selected</span>
          ) : toolbarHeader !== undefined ? (
            <span className={styles.toolbarHeader}>{toolbarHeader}</span>
          ) : null}
          <div className={styles.toolbarActions}>
            {hasSelection ? (
              <Button
                size="sm"
                variant="danger"
                leadingIcon="trash"
                onClick={() => confirmDelete.request(selectedRules)}
              >
                Delete ({selectedRules.length})
              </Button>
            ) : (
              toolbar
            )}
          </div>
        </div>
      ) : null}

      <div className={styles.tableScroll}>
        <div className={styles.headerRow} role="row">
          <span className={styles.expandCell} aria-hidden="true" />
          <span className={styles.handleCell} aria-hidden="true" />
          <span className={styles.checkboxCell}>
            <Checkbox
              aria-label="Select all rules"
              checked={allSelected ? true : someSelected ? "indeterminate" : false}
              onCheckedChange={toggleAll}
            />
          </span>
          <span>Name</span>
          <span>Condition</span>
          <span>Modifications</span>
        </div>

        {localRules.length === 0 ? (
          <div className={styles.empty}>No rules yet.</div>
        ) : (
          <DndContext
            sensors={sensors}
            collisionDetection={closestCenter}
            onDragStart={handleDragStart}
            onDragMove={handleDragMove}
            onDragCancel={handleDragCancel}
            onDragEnd={handleDragEnd}
            autoScroll={{ enabled: true, threshold: { x: 0, y: 0.2 } }}
          >
            <SortableContext
              items={renderedFlat.map((n) => n.rule.uid ?? n.rule.name)}
              strategy={noopStrategy}
            >
              {renderedFlat.map((n, i) => {
                const id = n.rule.uid ?? n.rule.name;
                return (
                  <Fragment key={id}>
                    {projection?.slotIndex === i ? (
                      <DropIndicator depth={projection.depth} />
                    ) : null}
                    <SortableTreeRow
                      id={id}
                      node={n}
                      depth={n.depth}
                      selected={isRowSelected(id)}
                      onToggleSelected={() => toggleSelection(id)}
                      onRowOpen={onRowOpen}
                      expanded={!activeId && expanded.has(id)}
                      onToggleExpanded={() => toggleExpanded(id)}
                    />
                  </Fragment>
                );
              })}
              {projection?.slotIndex === renderedFlat.length ? (
                <DropIndicator depth={projection.depth} />
              ) : null}
            </SortableContext>
            <DragOverlay dropAnimation={null}>
              {activeRule ? <DragPreview rule={activeRule} /> : null}
            </DragOverlay>
          </DndContext>
        )}
      </div>

      <ConfirmDeleteDialog
        state={confirmDelete.state}
        onCancel={confirmDelete.cancel}
        onConfirm={() => void confirmDelete.confirm()}
      />
    </div>
  );
}

function DropIndicator({ depth }: { depth: number }) {
  return (
    <div
      className={styles.dropIndicator}
      style={{ paddingLeft: 28 + 28 + 28 + depth * INDENT_PX + 12 }}
      aria-hidden="true"
    >
      <div className={styles.dropIndicatorLine} />
    </div>
  );
}

function DragPreview({ rule }: { rule: Rule }) {
  return (
    <div className={styles.dragPreview} role="presentation">
      <Icon name="grip" size={16} />
      <Code>{rule.name}</Code>
    </div>
  );
}

type SortableTreeRowProps = {
  id: string;
  node: FlatNode;
  depth: number;
  selected: boolean;
  onToggleSelected: () => void;
  onRowOpen: (r: Rule) => void;
  expanded: boolean;
  onToggleExpanded: () => void;
};

function SortableTreeRow({
  id,
  node,
  depth,
  selected,
  onToggleSelected,
  onRowOpen,
  expanded,
  onToggleExpanded,
}: SortableTreeRowProps) {
  const sortable = useSortable({ id });
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = sortable;
  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition: transition ?? undefined,
  };
  const enabled = node.rule.enabled !== false;
  const mods = node.rule.modifications ?? [];
  return (
    <div ref={setNodeRef} style={style} className={styles.rowOuter}>
      <div
        className={[
          styles.row,
          isDragging ? styles.rowDragging : "",
          selected ? styles.rowSelected : "",
        ]
          .filter(Boolean)
          .join(" ")}
        {...(!enabled ? { "data-disabled": "true" } : {})}
        {...(selected ? { "data-selected": "true" } : {})}
        onClick={(e) => {
          // Suppress row-open when the click started on a control inside
          // the row (drag handle, expand chevron, checkbox).
          const target = e.target as HTMLElement;
          if (target.closest("[data-drag-handle]")) return;
          if (target.closest("[data-expand-toggle]")) return;
          if (target.closest("[data-row-checkbox]")) return;
          onRowOpen(node.rule);
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            const target = e.target as HTMLElement;
            if (target.closest("[data-drag-handle]")) return;
            if (target.closest("[data-expand-toggle]")) return;
            if (target.closest("[data-row-checkbox]")) return;
            onRowOpen(node.rule);
          }
        }}
        tabIndex={0}
        role="row"
      >
        <button
          type="button"
          data-expand-toggle
          className={styles.expandBtn}
          aria-label={`Expand row ${node.rule.name}`}
          aria-expanded={expanded}
          onClick={(e) => {
            e.stopPropagation();
            onToggleExpanded();
          }}
        >
          <Icon name={expanded ? "chevron-down" : "chevron-right"} size={14} />
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
        {/* eslint-disable-next-line jsx-a11y/no-static-element-interactions, jsx-a11y/click-events-have-key-events -- Click-swallow shim so clicking the checkbox doesn't also open the editor; keyboard handling lives on the Checkbox inside. */}
        <span
          data-row-checkbox
          className={styles.checkboxCell}
          onClick={(e) => e.stopPropagation()}
        >
          <Checkbox
            aria-label={`Select rule ${node.rule.name}`}
            checked={selected}
            onCheckedChange={onToggleSelected}
          />
        </span>
        <span className={styles.nameCell}>
          {depth > 0 ? (
            <span
              className={styles.indent}
              style={{ width: depth * INDENT_PX }}
              aria-hidden="true"
            />
          ) : null}
          <Code>{node.rule.name}</Code>
        </span>
        <span className={styles.conditionCell}>
          {prettyCondition(node.rule.condition)}
        </span>
        <span className={styles.modsCell}>
          {mods.length === 0 ? (
            <span className={styles.comment}>—</span>
          ) : (
            mods.map((m, i) => (
              <Badge key={i} variant="neutral">
                {String((m[0] as string | number | null | undefined) ?? "")} {String((m[1] as string | number | null | undefined) ?? "")}
              </Badge>
            ))
          )}
        </span>
      </div>
      {expanded ? (
        <div className={styles.expandedRow}>
          <RowDetailPanel
            row={node.rule as unknown as Record<string, unknown>}
            objectType="rule"
            objectId={node.rule.uid}
          />
        </div>
      ) : null}
    </div>
  );
}
