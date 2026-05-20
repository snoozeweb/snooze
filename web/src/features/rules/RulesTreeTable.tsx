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
  pointerWithin,
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
import { EmptyState } from "@/shared/ui/EmptyState";
import { Menu, MenuContent, MenuItem, MenuTrigger } from "@/shared/ui/Menu";
import { RowDetailPanel } from "@/shared/ui/RowDetailPanel";
import { toast } from "@/shared/ui/toast/useToast";
import { prettyCondition } from "@/lib/condition/pretty";
import {
  ConfirmDeleteDialog,
  useConfirmDelete,
} from "@/shared/ui/resourceContextMenu";
import type { ParsedCondition } from "@/shared/ui/SearchBar";
import { SearchBar } from "@/shared/ui/SearchBar";
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
// is handled by the ghost row + the DragOverlay clone, not by transforming
// the surrounding rows — that approach (verticalListSortingStrategy) fights
// with our per-row depth changes and produces inconsistent drop animations
// when crossing parent boundaries.
const noopStrategy = () => null;

/** Per-row insertion direction selected from the "+ Add" menu next to a
 *  rule. The host page is responsible for translating this into the
 *  concrete RuleInsertion (parents, tree_order, siblingPatches).
 *
 *  - "above" / "below": same-level sibling of the row, immediately before
 *    or after it. Existing siblings whose tree_order would collide with
 *    the new slot get bumped up by one.
 *  - "child": appended after the row's existing children at depth+1. No
 *    sibling shifts needed.
 */
export type InsertDirection = "above" | "below" | "child";

export type RulesTreeTableProps = {
  rules: Rule[];
  onRowOpen: (r: Rule) => void;
  /** Per-row "+ Add" menu callback. Fired with the anchor row and the
   *  chosen direction; the page computes the actual RuleInsertion and
   *  opens the editor. Omit to hide the menu (e.g. for read-only views). */
  onInsert?: (anchor: Rule, direction: InsertDirection) => void;
  /** Persistent toolbar slot — host's "New" button etc. Bulk-selection
   *  mode replaces this with the delete action, just like DataTable.
   *  When the page renders the toolbar itself in the tabbed-header right
   *  slot, leave this unset and pass `selectedKeys` / `onSelectionChange`
   *  so the page can mirror the selection state up. */
  toolbar?: React.ReactNode;
  toolbarHeader?: React.ReactNode;
  /** Controlled-selection mode: when both are provided, RulesTreeTable
   *  uses the page's state and skips rendering its internal bulk-action
   *  toolbar — the page is expected to render bulk actions externally
   *  (e.g. in the TabList's rightSlot). */
  selectedKeys?: ReadonlySet<string>;
  onSelectionChange?: (next: Set<string>) => void;
  /** Same shape as DataTable's `search` prop. When provided, a SearchBar
   *  renders above the tree; rules already on the page are filtered
   *  server-side via ?q=. */
  search?: {
    value: string;
    onChange: (next: { text: string; condition: ParsedCondition | null }) => void;
    collection?: string;
    placeholder?: string;
  };
  /** When true, drag handles are hidden and the DnD context is suppressed.
   *  Reordering a filtered subset of the tree would have indeterminate
   *  semantics (the cursor's "between rows" position doesn't reliably
   *  correspond to anything in the full tree), so we disable it. */
  searchActive?: boolean;
  /** Optional action rendered inside the "No rules yet" empty state — the
   *  page's "+ New" affordance lives here so operators have a clear entry
   *  point even when the table is brand new. */
  emptyAction?: React.ReactNode;
  /** When provided, the table defers PATCHing the new (parents, tree_order)
   *  to the host. Return value is ignored — the host is expected to either
   *  fire mutations directly or accumulate them for a later Validate step.
   *  When undefined, the table fires mutations itself (legacy behavior). */
  onCommitPatches?: (patches: Array<{ uid: string; parents: string[]; tree_order: number }>) => void;
  /** When set, the local optimistic state resets back to the `rules` prop.
   *  Used by the host's "Cancel" action to undo all uncommitted drops
   *  without waiting for a server refetch. The host increments this on
   *  each cancel; any change triggers the reset effect. */
  localResetCounter?: number;
  /** When true, the table renders with a "pending changes" affordance
   *  (yellow tint, etc.) to signal that uncommitted drops are present. */
  pending?: boolean;
};

export function RulesTreeTable({
  rules,
  onRowOpen,
  onInsert,
  toolbar,
  toolbarHeader,
  selectedKeys: selectedKeysProp,
  onSelectionChange,
  search,
  searchActive = false,
  emptyAction,
  onCommitPatches,
  localResetCounter,
  pending = false,
}: RulesTreeTableProps) {
  const update = Rules.useUpdate();
  const remove = Rules.useRemove();

  // Local mirror of the prop, so we can apply drops optimistically before
  // the network round-trip — otherwise dnd-kit animates the row back to its
  // origin and only the post-refetch render shows the new order, which
  // reads as a flicker.
  const [localRules, setLocalRules] = useState<Rule[]>(rules);
  useEffect(() => setLocalRules(rules), [rules]);
  // The host's "Cancel" action increments localResetCounter to roll back
  // optimistic drops without waiting for a server refetch.
  useEffect(() => {
    if (localResetCounter !== undefined) setLocalRules(rules);
    // localResetCounter is deliberately the only trigger — rules is captured
    // by the rules-effect above.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [localResetCounter]);

  const { roots } = useMemo(() => buildTree(localRules), [localRules]);
  const fullFlat = useMemo(() => flattenTree(roots), [roots]);

  // Selection state. Toggling a parent toggles its full subtree.
  // Controlled (page-supplied) vs uncontrolled (internal) is decided by
  // whether the host passed selectedKeys+onSelectionChange.
  const isControlled = selectedKeysProp !== undefined && onSelectionChange !== undefined;
  const [internalSelected, setInternalSelected] = useState<Set<string>>(() => new Set());
  const selected: ReadonlySet<string> = isControlled
    ? selectedKeysProp
    : internalSelected;
  const setSelected = useCallback(
    (next: Set<string> | ((prev: ReadonlySet<string>) => Set<string>)) => {
      if (isControlled && onSelectionChange) {
        const resolved =
          typeof next === "function"
            ? next(selectedKeysProp ?? new Set<string>())
            : next;
        onSelectionChange(resolved);
      } else {
        setInternalSelected((prev) =>
          typeof next === "function" ? next(prev) : next,
        );
      }
    },
    [isControlled, onSelectionChange, selectedKeysProp],
  );
  // Cull stale selections when the underlying set of rules changes.
  // Only emits a new set when something actually drops out — otherwise we'd
  // bounce `selectedKeys` through the parent every render and trigger an
  // infinite update loop in controlled mode.
  useEffect(() => {
    const alive = new Set(localRules.map((r) => r.uid ?? r.name));
    let needsCull = false;
    for (const id of selected) {
      if (!alive.has(id)) {
        needsCull = true;
        break;
      }
    }
    if (!needsCull) return;
    const next = new Set<string>();
    for (const id of selected) if (alive.has(id)) next.add(id);
    setSelected(next);
    // selected is intentionally excluded from deps — the effect should
    // re-evaluate against the new rule set, not whenever the parent passes
    // a fresh Set ref for an unchanged selection.
    // eslint-disable-next-line react-hooks/exhaustive-deps
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
    [fullFlat, setSelected],
  );

  const allSelected =
    localRules.length > 0 && localRules.every((r) => selected.has(r.uid ?? r.name));
  const someSelected = localRules.some((r) => selected.has(r.uid ?? r.name));
  const toggleAll = useCallback(() => {
    if (allSelected) setSelected(new Set());
    else setSelected(new Set(localRules.map((r) => r.uid ?? r.name)));
  }, [allSelected, localRules, setSelected]);

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
  // tracks horizontal drift so we can project nesting depth. `dropAfter`
  // is true when the cursor is in the lower half of the over row — the
  // drop slot becomes "after this row" rather than "before this row",
  // which makes "drag onto X + drag right = child of X" feel natural.
  const [activeId, setActiveId] = useState<string | null>(null);
  const [overId, setOverId] = useState<string | null>(null);
  const [offsetX, setOffsetX] = useState(0);
  const [dropAfter, setDropAfter] = useState(false);

  // The PointerSensor activates only after 6px of movement so plain row
  // clicks (which open the editor drawer) still work.
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  // While dragging, we render the FULL list (active subtree included) so
  // the cursor's frame of reference doesn't shift the moment a drag begins
  // — if we filtered the active row out, the row below it would jump up
  // into the cursor's position and a single pixel of motion would
  // immediately cross a row boundary. Instead we mark the active subtree
  // with `data-in-active-subtree` and let CSS decide how to display it
  // based on whether the cursor has committed to a different slot.
  const subtreeIds = useMemo(
    () => (activeId ? collectSubtreeIds(fullFlat, activeId) : new Set<string>()),
    [activeId, fullFlat],
  );
  const renderedFlat = fullFlat;

  const activeRule = useMemo(() => {
    if (!activeId) return null;
    return fullFlat.find((n) => (n.rule.uid ?? n.rule.name) === activeId)?.rule ?? null;
  }, [activeId, fullFlat]);

  // Project the drop target — both the slot index in `renderedFlat` (where
  // the ghost row should render) and the (parentId, depth) the active row
  // will adopt.
  //
  // No-move state: when the cursor is still over the active row (or any of
  // its descendants), the user hasn't committed to moving anywhere. We
  // surface that as projection=null upstream so the ghost row isn't
  // rendered and the active subtree stays visible-dimmed in place.
  const projection = useMemo(() => {
    if (!activeId) return null;
    // No-move state — the user hasn't committed to a target row yet:
    //   - overId === null: drag just started, no `dragMove` event fired
    //     yet. CRITICAL to return null here: with projection !== null,
    //     the active row's outer wrapper goes display:none, which
    //     collapses its bounding rect to (0,0,0,0) and makes
    //     <DragOverlay> snap to the top-left of the window.
    //   - cursor still over the active subtree itself: dragging onto
    //     yourself is a no-op move.
    if (overId === null) return null;
    if (subtreeIds.has(overId)) return null;
    // Sibling-shift math runs against the list AS IT WOULD BE after the
    // active subtree is lifted out (`projectionList`). Rendering, on the
    // other hand, happens against the FULL renderedFlat (which still
    // contains the subtree) — so we carry two slot indices.
    const projectionList = renderedFlat.filter(
      (n) => !subtreeIds.has(n.rule.uid ?? n.rule.name),
    );
    const idx = projectionList.findIndex(
      (n) => (n.rule.uid ?? n.rule.name) === overId,
    );
    // dropAfter swaps the slot from "before this row" to "after this
    // row" so "drag onto X + drag right" projects as "child of X"
    // instead of "child of X's previous sibling". When the user is on
    // the lower half of the LAST row, slot becomes projectionList.length,
    // which is the "drop at the very end" semantic — no separate
    // sentinel droppable required.
    const slotInProjection =
      idx < 0 ? projectionList.length : dropAfter ? idx + 1 : idx;
    const { parentId, depth } = projectDrop(
      projectionList,
      slotInProjection,
      offsetX,
      INDENT_PX,
    );
    const anchorNode = projectionList[slotInProjection];
    const slotInRendered = anchorNode
      ? renderedFlat.findIndex(
          (n) => (n.rule.uid ?? n.rule.name) === (anchorNode.rule.uid ?? anchorNode.rule.name),
        )
      : renderedFlat.length;
    return {
      parentId,
      depth,
      slotInProjection,
      slotInRendered: slotInRendered < 0 ? renderedFlat.length : slotInRendered,
    };
  }, [activeId, overId, renderedFlat, subtreeIds, offsetX, dropAfter]);

  const handleDragStart = useCallback((e: DragStartEvent) => {
    setActiveId(e.active.id as string);
    setOverId(null);
    setOffsetX(0);
    setDropAfter(false);
    // Force-close every expanded details panel — leaving them open while
    // rows are reshuffling produces the worst kind of layout thrash.
    setExpanded(new Set());
  }, []);

  const handleDragMove = useCallback((e: DragMoveEvent) => {
    setOffsetX(e.delta.x);
    // Sticky overId — only adopt a new "over" target when dnd-kit reports
    // a real row under the cursor. The cursor occasionally crosses
    // sub-pixel gaps between adjacent row rects where pointerWithin
    // returns no match; without this guard, projection flips to null
    // for a frame and the ghost row disappears mid-drag.
    const id = (e.over?.id as string | undefined) ?? null;
    if (id !== null && e.over?.rect) {
      setOverId(id);
      const activator = e.activatorEvent as MouseEvent | PointerEvent;
      const cursorY = activator.clientY + e.delta.y;
      const overRect = e.over.rect;
      const mid = (overRect.top + overRect.bottom) / 2;
      setDropAfter(cursorY > mid);
    }
    // When over is null we deliberately KEEP the previous overId so the
    // projection (and ghost row) stay put while the cursor crosses the
    // gap. Once it lands on the next row, dragMove fires again with a
    // valid id and we adopt it.
  }, []);

  const handleDragCancel = useCallback(() => {
    setActiveId(null);
    setOverId(null);
    setOffsetX(0);
    setDropAfter(false);
  }, []);

  const handleDragEnd = useCallback(
    (e: DragEndEvent) => {
      const draggedId = e.active.id as string;
      setActiveId(null);
      setOverId(null);
      setOffsetX(0);
      setDropAfter(false);
      if (!projection) return;

      // Reconstruct the new flat order. The active row + its subtree should
      // land contiguously at the projected slot in the "remainder" list
      // (fullFlat with the subtree removed). projection.slotInProjection is
      // already expressed in those coordinates.
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

      const insertAtSafe = Math.min(
        Math.max(projection.slotInProjection, 0),
        remainder.length,
      );

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

      // Defer to the host when it asked to manage commits (Validate /
      // Cancel staging). Otherwise fire mutations inline (legacy path).
      if (onCommitPatches) {
        onCommitPatches(patches);
      } else {
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
      }
    },
    [fullFlat, projection, update, onCommitPatches],
  );

  const isRowSelected = (id: string) => selected.has(id);

  const hasSelection = selectedRules.length > 0;
  // When selection is controlled by the host, it also renders its own
  // bulk-action surface (typically in the tabbed-header right slot), so
  // we skip the internal toolbar entirely to avoid double-rendering.
  const renderToolbar =
    !isControlled && (toolbar !== undefined || toolbarHeader !== undefined || hasSelection);

  return (
    <div className={styles.wrap}>
      {search ? (
        <SearchBar
          value={search.value}
          onChange={(c) => search.onChange({ text: c.text, condition: c.condition })}
          {...(search.collection ? { collection: search.collection } : {})}
          {...(search.placeholder ? { placeholder: search.placeholder } : {})}
        />
      ) : null}
      {searchActive ? (
        <div className={styles.searchHint} role="status">
          Drag-and-drop reordering is disabled while a search filter is
          active — clear the search to rearrange rules.
        </div>
      ) : null}
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

      <div
        className={styles.tableScroll}
        {...(pending ? { "data-pending": "true" } : {})}
        {...(activeId ? { "data-dragging": "true" } : {})}
      >
        <div className={styles.headerRow} role="row">
          <span className={styles.expandCell} aria-hidden="true" />
          <span className={styles.handleCell} aria-hidden="true" />
          <span className={styles.checkboxCell}>
            <Checkbox
              aria-label="Select all rules"
              checked={allSelected ? true : someSelected ? "indeterminate" : false}
              onCheckedChange={toggleAll}
              disabled={pending}
            />
          </span>
          <span>Name</span>
          <span>Condition</span>
          <span>Modifications</span>
          <span className={styles.addCell} aria-hidden="true" />
        </div>

        {localRules.length === 0 ? (
          <div className={styles.empty}>
            <EmptyState
              icon="file-text"
              title="No rules yet"
              description="Rules transform incoming alerts. Add one to get started."
              {...(emptyAction !== undefined ? { action: emptyAction } : {})}
            />
          </div>
        ) : searchActive ? (
          // Filtered view: skip the DndContext entirely — operators can read
          // and edit rules, but reordering is gated until the search clears.
          // The drag handles fall back to a static grip icon so the column
          // grid stays consistent with the unfiltered view.
          renderedFlat.map((n) => {
            const id = n.rule.uid ?? n.rule.name;
            return (
              <StaticTreeRow
                key={id}
                node={n}
                depth={n.depth}
                selected={isRowSelected(id)}
                onToggleSelected={() => toggleSelection(id)}
                onRowOpen={onRowOpen}
                {...(onInsert && !pending ? { onInsert } : {})}
                expanded={expanded.has(id)}
                onToggleExpanded={() => toggleExpanded(id)}
                selectionLocked={pending}
              />
            );
          })
        ) : (
          <DndContext
            sensors={sensors}
            // pointerWithin avoids the closestCenter flicker at row
            // midpoints: the "over" row is whichever rect the cursor is
            // actually inside, not whichever rect's CENTER is closest.
            // The result is stable across 1px movements — no oscillation
            // between two rows whose centers happen to be equidistant.
            collisionDetection={pointerWithin}
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
                const inActiveSubtree = subtreeIds.has(id);
                return (
                  <Fragment key={id}>
                    {projection?.slotInRendered === i && activeRule ? (
                      <GhostRow rule={activeRule} depth={projection.depth} />
                    ) : null}
                    <SortableTreeRow
                      id={id}
                      node={n}
                      depth={n.depth}
                      selected={isRowSelected(id)}
                      onToggleSelected={() => toggleSelection(id)}
                      onRowOpen={onRowOpen}
                      // Hide the per-row "+ Add" menu while there are
                      // uncommitted reorders — inserting a new rule mid-
                      // edit would shift siblings the pending patches
                      // don't account for and corrupt the staged state.
                      {...(onInsert && !pending ? { onInsert } : {})}
                      expanded={!activeId && expanded.has(id)}
                      onToggleExpanded={() => toggleExpanded(id)}
                      // Active subtree rows: when the cursor is still over
                      // them (projection=null), stay visible-dimmed in
                      // place; once the cursor commits to a different slot
                      // (projection is set), collapse out of the layout so
                      // the ghost can take their slot without growing the
                      // table.
                      activeSubtreeMode={
                        !inActiveSubtree
                          ? "none"
                          : projection === null
                          ? "dim"
                          : "collapsed"
                      }
                      // Lock selection while pending — the right header
                      // slot is occupied by Cancel/Save, so bulk-action
                      // affordances aren't reachable until commit anyway.
                      selectionLocked={pending}
                    />
                  </Fragment>
                );
              })}
              {projection?.slotInRendered === renderedFlat.length && activeRule ? (
                <GhostRow rule={activeRule} depth={projection.depth} />
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

// GhostRow — a placeholder rendered at the projected drop slot. It mirrors
// the regular row's grid layout so the surrounding columns stay aligned,
// but its name cell shows the dragged rule's name at the *projected* depth.
// This is the "you'll land here, at this nesting level" feedback that a
// thin indicator line alone failed to convey.
function GhostRow({ rule, depth }: { rule: Rule; depth: number }) {
  return (
    <div className={`${styles.row} ${styles.ghostRow}`} aria-hidden="true">
      <span className={styles.expandCell} />
      <span className={styles.handleCell}>
        <Icon name="grip" size={16} />
      </span>
      <span className={styles.checkboxCell} />
      <span className={styles.nameCell}>
        {depth > 0 ? (
          <span
            className={styles.indent}
            style={{ width: depth * INDENT_PX }}
            aria-hidden="true"
          />
        ) : null}
        <Code>{rule.name}</Code>
      </span>
      <span className={styles.conditionCell} />
      <span className={styles.modsCell} />
      <span className={styles.addCell} />
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

// AddRuleMenu — the per-row "+ Add" dropdown. Calling `onPick` fires the
// page's onInsert callback, which is responsible for translating the
// chosen direction into a concrete RuleInsertion (parents, tree_order,
// siblingPatches) and opening the RuleEditor in create mode.
function AddRuleMenu({
  anchorName,
  onPick,
}: {
  anchorName: string;
  onPick: (direction: InsertDirection) => void;
}) {
  return (
    <Menu>
      <MenuTrigger>
        <button
          type="button"
          data-add-rule
          className={styles.addBtn}
          aria-label={`Add a rule near ${anchorName}`}
          // The wrapping <div role="row" onClick> opens the editor for the
          // anchor row — we don't want that here.
          onClick={(e) => e.stopPropagation()}
        >
          <Icon name="plus" size={14} />
        </button>
      </MenuTrigger>
      <MenuContent>
        <MenuItem leadingIcon="chevron-up" onSelect={() => onPick("above")}>
          Add rule above
        </MenuItem>
        <MenuItem leadingIcon="chevron-down" onSelect={() => onPick("below")}>
          Add rule below
        </MenuItem>
        <MenuItem leadingIcon="chevron-right" onSelect={() => onPick("child")}>
          Add child rule
        </MenuItem>
      </MenuContent>
    </Menu>
  );
}

// StaticTreeRow — same visual as SortableTreeRow but without dnd-kit
// hooks. Rendered when a search filter is active, since rearranging a
// filtered subset of the tree has no well-defined semantics.
function StaticTreeRow({
  node,
  depth,
  selected,
  onToggleSelected,
  onRowOpen,
  onInsert,
  expanded,
  onToggleExpanded,
  selectionLocked = false,
}: Omit<SortableTreeRowProps, "id">) {
  const enabled = node.rule.enabled !== false;
  const mods = node.rule.modifications ?? [];
  return (
    <div className={styles.rowOuter}>
      <div
        className={[
          styles.row,
          selected ? styles.rowSelected : "",
        ]
          .filter(Boolean)
          .join(" ")}
        {...(!enabled ? { "data-disabled": "true" } : {})}
        {...(selected ? { "data-selected": "true" } : {})}
        onClick={(e) => {
          // Radix dropdowns and other portals bubble their click events
          // through the React tree, not the DOM tree, so a click on a
          // portaled menu item lands here. Drop those — only DOM
          // descendants of this row should open the editor.
          if (!e.currentTarget.contains(e.target as Node)) return;
          const target = e.target as HTMLElement;
          if (target.closest("[data-expand-toggle]")) return;
          if (target.closest("[data-row-checkbox]")) return;
          if (target.closest("[data-add-rule]")) return;
          onRowOpen(node.rule);
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            if (!e.currentTarget.contains(e.target as Node)) return;
            const target = e.target as HTMLElement;
            if (target.closest("[data-expand-toggle]")) return;
            if (target.closest("[data-row-checkbox]")) return;
            if (target.closest("[data-add-rule]")) return;
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
        <span className={styles.handle} aria-hidden="true">
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
            disabled={selectionLocked}
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
        <span className={styles.addCell}>
          {onInsert ? (
            <AddRuleMenu
              anchorName={node.rule.name}
              onPick={(direction) => onInsert(node.rule, direction)}
            />
          ) : null}
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

type SortableTreeRowProps = {
  id: string;
  node: FlatNode;
  depth: number;
  selected: boolean;
  onToggleSelected: () => void;
  onRowOpen: (r: Rule) => void;
  onInsert?: (anchor: Rule, direction: InsertDirection) => void;
  expanded: boolean;
  onToggleExpanded: () => void;
  /** Visual mode for active-subtree rows while a drag is in flight:
   *   - "none":      not part of the dragged subtree, render normally
   *   - "dim":       cursor still over the subtree (no-move), stay
   *                  visible at reduced opacity so the cursor frame
   *                  doesn't shift
   *   - "collapsed": cursor has committed to a different slot, hide
   *                  via display:none so the ghost takes the freed
   *                  vertical space and the table stays the same size */
  activeSubtreeMode?: "none" | "dim" | "collapsed";
  /** When true, the row's selection checkbox is rendered disabled —
   *  used while uncommitted drag-and-drop changes are pending so the
   *  user focuses on committing/cancelling before doing other actions. */
  selectionLocked?: boolean;
};

function SortableTreeRow({
  id,
  node,
  depth,
  selected,
  onToggleSelected,
  onRowOpen,
  onInsert,
  expanded,
  onToggleExpanded,
  activeSubtreeMode = "none",
  selectionLocked = false,
}: SortableTreeRowProps) {
  const sortable = useSortable({ id });
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = sortable;
  // CRITICAL: skip the dnd-kit transform on the active (source) row. With
  // <DragOverlay> rendering a floating clone that follows the cursor, the
  // SOURCE row must stay at its original position — otherwise both the
  // transform-translated source and the overlay clone race for the same
  // screen position, producing a "row jumping to the top-left corner +
  // flickering" effect every time the user starts a drag.
  const style: React.CSSProperties = isDragging
    ? {}
    : {
        transform: CSS.Transform.toString(transform),
        transition: transition ?? undefined,
      };
  const enabled = node.rule.enabled !== false;
  const mods = node.rule.modifications ?? [];
  return (
    <div
      ref={setNodeRef}
      style={style}
      className={[
        styles.rowOuter,
        activeSubtreeMode === "collapsed" ? styles.rowOuterCollapsed : "",
      ]
        .filter(Boolean)
        .join(" ")}
    >
      <div
        className={[
          styles.row,
          activeSubtreeMode === "dim" ? styles.rowDraggingDim : "",
          selected ? styles.rowSelected : "",
        ]
          .filter(Boolean)
          .join(" ")}
        {...(!enabled ? { "data-disabled": "true" } : {})}
        {...(selected ? { "data-selected": "true" } : {})}
        onClick={(e) => {
          // Suppress row-open when the click started on a control inside
          // the row (drag handle, expand chevron, checkbox, add menu) OR
          // bubbled through React's tree from a portaled descendant
          // (Radix dropdowns) — those aren't in the row's DOM subtree.
          if (!e.currentTarget.contains(e.target as Node)) return;
          const target = e.target as HTMLElement;
          if (target.closest("[data-drag-handle]")) return;
          if (target.closest("[data-expand-toggle]")) return;
          if (target.closest("[data-row-checkbox]")) return;
          if (target.closest("[data-add-rule]")) return;
          onRowOpen(node.rule);
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            if (!e.currentTarget.contains(e.target as Node)) return;
            const target = e.target as HTMLElement;
            if (target.closest("[data-drag-handle]")) return;
            if (target.closest("[data-expand-toggle]")) return;
            if (target.closest("[data-row-checkbox]")) return;
            if (target.closest("[data-add-rule]")) return;
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
            disabled={selectionLocked}
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
        <span className={styles.addCell}>
          {onInsert ? (
            <AddRuleMenu
              anchorName={node.rule.name}
              onPick={(direction) => onInsert(node.rule, direction)}
            />
          ) : null}
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
