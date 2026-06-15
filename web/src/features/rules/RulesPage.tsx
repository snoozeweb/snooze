import { useCallback, useEffect, useMemo, useState } from "react";
import { useBlocker, useNavigate, useSearch } from "@tanstack/react-router";
import { Button } from "@/shared/ui/Button";
import { DataTable } from "@/shared/ui/DataTable";
import type { ContextMenuItem } from "@/shared/ui/DataTableContextMenu";
import { EmptyState } from "@/shared/ui/EmptyState";
import { RowDetailPanel } from "@/shared/ui/RowDetailPanel";
import { TabList, TabPanel, TabTrigger, Tabs } from "@/shared/ui/Tabs";
import { useTableSearch } from "@/shared/hooks/useTableSearch";
import {
  buildResourceContextMenu,
  ConfirmDeleteDialog,
  useConfirmDelete,
} from "@/shared/ui/resourceContextMenu";
import { AggregateRules, Rules } from "./api";
import { RuleEditor, type RuleInsertion } from "./RuleEditor";
import { RulesTreeTable, type InsertDirection } from "./RulesTreeTable";
import { aggregateRuleColumns } from "./columns";
import { ROOT } from "./tree";
import { ruleRowDisabled } from "./ruleUtils";
import type { AggregateRule, Rule } from "./types";
import styles from "./RulesPage.module.css";

type RulesSearch = {
  tab?: "rules" | "aggregates";
  uid?: string | undefined;
  page?: number;
  orderby?: string;
  asc?: boolean;
};

// TanStack Router's navigate types are locked to the registered route tree at
// build time. Casting through unknown avoids type errors when the route is
// locally constructed in tests and still works when fully registered.
type NavigateFn = (opts: {
  to: string;
  search: (prev: RulesSearch | undefined) => RulesSearch;
}) => Promise<void>;

const PAGE_SIZE = 50;

// Design decision (Phase 6 plan): no virtualization and no branch collapse.
// Realistic rule counts are in the tens, so TanStack Virtual overhead is not
// warranted. Branch collapse was audited and rejected: operators rely on
// seeing the full rule tree at a glance to reason about evaluation order.
export function RulesPage() {
  // useSearch with strict:false returns the validated search params; cast for local type.
  const search = useSearch({ strict: false }) as unknown as RulesSearch;
  const navigate = useNavigate();

  const tab = search.tab ?? "rules";
  const page = search.page ?? 1;
  const orderby = search.orderby ?? "name";
  const asc = search.asc ?? true;
  const detailUid = search.uid;
  const [creating, setCreating] = useState(false);
  // Insertion target staged from a per-row "+ Add" menu pick. Cleared
  // when the editor closes. When set, RuleEditor receives it as `insertion`
  // and applies the necessary sibling shifts + parent/tree_order on POST.
  const [pendingInsertion, setPendingInsertion] = useState<RuleInsertion | null>(null);

  const updateSearch = useCallback(
    (next: RulesSearch) => {
      void (navigate as unknown as NavigateFn)({
        to: "/web/rules",
        search: (prev: RulesSearch | undefined) => {
          const merged = { ...(prev ?? {}), ...next };
          // exactOptionalPropertyTypes: remove keys set to undefined rather than keeping them
          if (merged.uid === undefined) {
            const { uid: _uid, ...rest } = merged;
            return rest as RulesSearch;
          }
          return merged as RulesSearch;
        },
      });
    },
    [navigate],
  );

  const resource = tab === "rules" ? Rules : AggregateRules;
  const isTree = tab === "rules";

  // Each tab carries its own search state — switching between Rules and
  // Aggregates preserves the query you typed in the other tab. The active
  // tab decides which filter is actually applied.
  const ruleSearch = useTableSearch({
    collection: "rule",
    placeholder: "name = … AND enabled = true",
  });
  const aggregateSearch = useTableSearch({
    collection: "aggregaterule",
    placeholder: "fields CONTAINS host",
    // Distinct URL key so the Rules-tab and Aggregates-tab queries don't
    // collide in the address bar (both bars persist independently).
    paramKey: "aggSearch",
    onFilterChange: () => {
      if (page !== 1) updateSearch({ page: 1 });
    },
  });

  // Rules tab: load the full set (limit=1000) so the tree component can
  // build the parent/child hierarchy client-side without juggling
  // pagination across levels. Aggregates tab keeps the paginated table.
  const list = resource.useList(
    isTree
      ? {
          limit: 1000,
          orderby: "tree_order",
          asc: true,
          ...(ruleSearch.q ? { q: ruleSearch.q } : {}),
        }
      : {
          offset: (page - 1) * PAGE_SIZE,
          limit: PAGE_SIZE,
          orderby,
          asc,
          ...(aggregateSearch.q ? { q: aggregateSearch.q } : {}),
        },
  );

  const editorPlugin: "rule" | "aggregaterule" = tab === "rules" ? "rule" : "aggregaterule";

  const removeRule = Rules.useRemove();
  const removeAggregate = AggregateRules.useRemove();
  const updateRule = Rules.useUpdate();
  // Selection state is held per-tab so switching between Rules and
  // Aggregates doesn't accidentally cross-contaminate.
  const [ruleSelected, setRuleSelected] = useState<Set<string>>(new Set());
  const [aggregateSelected, setAggregateSelected] = useState<Set<string>>(new Set());

  // ── Drag-and-drop staging ────────────────────────────────────────────
  // Drops accumulate as a Map keyed by uid (last write wins so dragging
  // the same rule multiple times collapses to a single PATCH). The host
  // displays a yellow "X pending changes" banner with Validate / Cancel
  // until the user commits or rolls back.
  type PendingPatch = { parents: string[]; tree_order: number };
  const [pendingPatches, setPendingPatches] = useState<Map<string, PendingPatch>>(new Map());
  const [savingPending, setSavingPending] = useState(false);
  const [resetCounter, setResetCounter] = useState(0);
  const pendingCount = pendingPatches.size;

  const accumulatePatches = useCallback(
    (patches: { uid: string; parents: string[]; tree_order: number }[]) => {
      setPendingPatches((prev) => {
        const next = new Map(prev);
        for (const p of patches) {
          next.set(p.uid, { parents: p.parents, tree_order: p.tree_order });
        }
        return next;
      });
    },
    [],
  );

  // Auto-cancel pending mode when the user has dragged things back to
  // exactly the server state — every accumulated patch now matches the
  // rule's (parents, tree_order) in the prop. Without this, dragging a
  // rule down and then back up would leave the table in "pending" with
  // a no-op patch list and force the user to click Cancel.
  useEffect(() => {
    if (pendingPatches.size === 0) return;
    const ruleByUid = new Map((list.data?.data ?? []).map((r) => [r.uid, r] as const));
    let allNoop = true;
    for (const [uid, patch] of pendingPatches) {
      const rule = ruleByUid.get(uid);
      if (!rule) {
        allNoop = false;
        break;
      }
      const prevParents = rule.parents ?? [];
      const sameParents =
        prevParents.length === patch.parents.length &&
        prevParents.every((p, i) => p === patch.parents[i]);
      const sameOrder = (rule.tree_order ?? 0) === patch.tree_order;
      if (!sameParents || !sameOrder) {
        allNoop = false;
        break;
      }
    }
    if (allNoop) setPendingPatches(new Map());
  }, [pendingPatches, list.data]);

  const validatePending = useCallback(async () => {
    if (pendingCount === 0) return;
    setSavingPending(true);
    try {
      const entries = Array.from(pendingPatches.entries());
      // Sequential rather than parallel: rule reorders all hit the same
      // collection and the SQL backends occasionally race when N parallel
      // PATCHes land at once (same shape of issue we hit in the per-row
      // insertion flow).
      for (const [uid, body] of entries) {
        await updateRule.mutateAsync({
          uid,
          body: { parents: body.parents, tree_order: body.tree_order } as Partial<Rule>,
        });
      }
      setPendingPatches(new Map());
    } finally {
      setSavingPending(false);
    }
  }, [pendingCount, pendingPatches, updateRule]);

  const cancelPending = useCallback(() => {
    setPendingPatches(new Map());
    setResetCounter((c) => c + 1);
  }, []);

  // Keyboard shortcuts for pending-drop mode: Esc cancels, Enter validates.
  // Guarded against form fields so a stray Enter in the search bar / editor
  // drawer doesn't fire the save unexpectedly.
  useEffect(() => {
    if (pendingCount === 0) return;
    function onKey(e: KeyboardEvent) {
      const target = e.target as HTMLElement | null;
      if (target) {
        const tag = target.tagName;
        if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || target.isContentEditable) {
          return;
        }
      }
      if (e.key === "Escape") {
        e.preventDefault();
        cancelPending();
      } else if (e.key === "Enter") {
        if (savingPending) return;
        e.preventDefault();
        void validatePending();
      }
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [pendingCount, savingPending, cancelPending, validatePending]);

  // Selection and pending edit mode are mutually exclusive surfaces —
  // the right header slot only has room for one set of actions at a
  // time. We clear any existing row selection whenever the user enters
  // pending mode so the "you have X selected" UI doesn't silently linger
  // behind the pending banner, and we lock the checkboxes (see below)
  // so they can't make a new selection until they Save or Cancel.
  useEffect(() => {
    if (pendingCount > 0 && ruleSelected.size > 0) {
      setRuleSelected(new Set());
    }
  }, [pendingCount, ruleSelected.size]);

  // Navigation guard — for BOTH in-app routing (TanStack Router) and
  // browser-level navigation (tab close, hard refresh, URL bar). The
  // router blocker fires our confirm() prompt when the user clicks a
  // sidebar link / hits Back, and `enableBeforeUnload` wires the same
  // intent into the beforeunload event for browser nav. Both are gated
  // on `pendingCount > 0` so unrelated navigation is unaffected.
  useBlocker({
    shouldBlockFn: () => {
      if (pendingCount === 0) return false;
      const ok = window.confirm(
        "You have unsaved rule reorders. Leave without saving? Click Cancel to stay and review.",
      );
      // shouldBlockFn returns TRUE to BLOCK. The confirm dialog returns
      // true when the user clicked "OK" (i.e. wants to leave), so we
      // invert.
      return !ok;
    },
    enableBeforeUnload: () => pendingCount > 0,
  });
  const confirmDeleteRule = useConfirmDelete<Rule>({
    onDelete: (uid) => removeRule.mutateAsync(uid),
    noun: "rule",
    onAfter: () => setRuleSelected(new Set()),
  });
  const confirmDelete = useConfirmDelete<AggregateRule>({
    onDelete: (uid) => removeAggregate.mutateAsync(uid),
    noun: "aggregate rule",
    onAfter: () => setAggregateSelected(new Set()),
  });

  const aggregateContextMenu = useCallback(
    (row: AggregateRule): ContextMenuItem[] =>
      buildResourceContextMenu(row, {
        onOpen: (r) => {
          if (r.uid) updateSearch({ uid: r.uid });
        },
        onDelete: (uid) => removeAggregate.mutateAsync(uid),
        requestDelete: (r) => confirmDelete.request([r]),
      }),
    [updateSearch, removeAggregate, confirmDelete],
  );

  const ruleContextMenu = useCallback(
    (row: Rule): ContextMenuItem[] =>
      buildResourceContextMenu(row, {
        onOpen: (r) => {
          if (r.uid) updateSearch({ uid: r.uid });
        },
        onDelete: (uid) => removeRule.mutateAsync(uid),
        requestDelete: (r) => confirmDeleteRule.request([r]),
      }),
    [updateSearch, removeRule, confirmDeleteRule],
  );

  const aggregateBulkActions = useCallback(
    (rows: AggregateRule[]) => (
      <Button
        size="sm"
        variant="danger"
        leadingIcon="trash"
        onClick={() => confirmDelete.request(rows)}
      >
        Delete ({rows.length})
      </Button>
    ),
    [confirmDelete],
  );

  // Translate a per-row "+ Add above/below/as child" click into a concrete
  // RuleInsertion the editor can apply. Sibling shifts are computed against
  // the currently-loaded rules — assumes the tree is fully loaded
  // (limit=1000), which matches the Rules tab's load.
  const handleInsert = useCallback(
    (anchor: Rule, direction: InsertDirection) => {
      const all = list.data?.data ?? [];
      // For sibling moves (above / below) the new rule shares the anchor's
      // parents and inserts at tree_order = anchor.tree_order (above) or
      // anchor.tree_order + 1 (below). Existing siblings whose tree_order
      // is now occupied get bumped by 1.
      if (direction === "above" || direction === "below") {
        const parents = anchor.parents ?? [];
        const parentKey = parents[0] ?? ROOT;
        const anchorOrder = anchor.tree_order ?? 0;
        const targetOrder = direction === "above" ? anchorOrder : anchorOrder + 1;
        const siblings = all.filter(
          (r) => (r.parents?.[0] ?? ROOT) === parentKey && r.uid !== undefined,
        );
        const siblingPatches = siblings
          .filter((r) => (r.tree_order ?? 0) >= targetOrder)
          .map((r) => ({ uid: r.uid!, tree_order: (r.tree_order ?? 0) + 1 }));
        setPendingInsertion({ parents, tree_order: targetOrder, siblingPatches });
        setCreating(true);
        return;
      }
      // "Add as child" — the new rule becomes the first child of the anchor.
      // We push it to position 0 and bump existing children. (Alternative:
      // append at the end; chose first-position so the new rule shows up
      // immediately under its parent when the parent's row is expanded.)
      const anchorUid = anchor.uid;
      if (!anchorUid) return;
      const children = all.filter(
        (r) => (r.parents?.[0] ?? ROOT) === anchorUid && r.uid !== undefined,
      );
      const siblingPatches = children.map((r) => ({
        uid: r.uid!,
        tree_order: (r.tree_order ?? 0) + 1,
      }));
      setPendingInsertion({
        parents: [anchorUid],
        tree_order: 0,
        siblingPatches,
      });
      setCreating(true);
    },
    [list.data],
  );

  // Selected rows for the active tab — used to render bulk actions in the
  // tabbed-header right slot.
  const ruleRows = useMemo(() => list.data?.data ?? [], [list.data]);
  const aggregateRows = useMemo(() => list.data?.data ?? [], [list.data]);
  const selectedRuleRows = useMemo(
    () => ruleRows.filter((r) => ruleSelected.has(r.uid ?? r.name)),
    [ruleRows, ruleSelected],
  );
  const selectedAggregateRows = useMemo(
    () => aggregateRows.filter((r) => aggregateSelected.has(r.uid ?? r.name)),
    [aggregateRows, aggregateSelected],
  );
  // Toolbar pieces: `header` is the count-or-selection text shown to the
  // left of `actions`; `actions` is the buttons cluster. Both sit on the
  // same row as the SearchBar inside each tab's table component, so we no
  // longer push them into the TabList's right slot.
  const rulesToolbarHeader =
    pendingCount > 0
      ? `${pendingCount} pending change${pendingCount === 1 ? "" : "s"}`
      : selectedRuleRows.length > 0
        ? `${selectedRuleRows.length} selected`
        : `${list.data?.meta.total ?? 0} rules`;
  // Toolbar "+ New" — always available (in non-pending, non-selection
  // states). New rules are appended at the end of the root level so the
  // existing tree stays put; no sibling shifts are required, which keeps
  // the no-anchor flow trivial relative to the per-row "+ Add" menu.
  const startNewRootRule = useCallback(() => {
    const rootCount = ruleRows.filter((r) => (r.parents?.length ?? 0) === 0).length;
    setPendingInsertion({ parents: [], tree_order: rootCount, siblingPatches: [] });
    setCreating(true);
  }, [ruleRows]);
  const rulesToolbarActions =
    pendingCount > 0 ? (
      <>
        <Button size="sm" variant="ghost" onClick={cancelPending} disabled={savingPending}>
          Cancel
        </Button>
        <Button
          size="sm"
          variant="primary"
          leadingIcon="check"
          onClick={() => void validatePending()}
          loading={savingPending}
          disabled={savingPending}
        >
          Save changes
        </Button>
      </>
    ) : selectedRuleRows.length > 0 ? (
      <Button
        size="sm"
        variant="danger"
        leadingIcon="trash"
        onClick={() => confirmDeleteRule.request(selectedRuleRows)}
      >
        Delete ({selectedRuleRows.length})
      </Button>
    ) : (
      <Button
        size="sm"
        variant="primary"
        leadingIcon="plus"
        onClick={startNewRootRule}
        disabled={list.isPending}
      >
        New
      </Button>
    );
  const aggregateToolbarHeader =
    selectedAggregateRows.length > 0
      ? `${selectedAggregateRows.length} selected`
      : `${list.data?.meta.total ?? 0} aggregate rules`;
  const aggregateToolbarActions =
    selectedAggregateRows.length > 0 ? (
      aggregateBulkActions(selectedAggregateRows)
    ) : (
      <Button size="sm" variant="primary" leadingIcon="plus" onClick={() => setCreating(true)}>
        New
      </Button>
    );

  return (
    <div className={styles.page}>
      <Tabs
        value={tab}
        onValueChange={(v) => updateSearch({ tab: v as "rules" | "aggregates", page: 1 })}
      >
        <TabList>
          <TabTrigger value="rules">Rules</TabTrigger>
          <TabTrigger value="aggregates">Aggregates</TabTrigger>
        </TabList>
        <TabPanel value={tab}>
          {isTree ? (
            <RulesTreeTable
              rules={ruleRows}
              onRowOpen={(row) => {
                if (row.uid) updateSearch({ uid: row.uid });
              }}
              onInsert={handleInsert}
              selectedKeys={ruleSelected}
              onSelectionChange={setRuleSelected}
              search={ruleSearch.searchProp}
              searchActive={ruleSearch.q !== undefined}
              onCommitPatches={accumulatePatches}
              localResetCounter={resetCounter}
              pending={pendingCount > 0}
              toolbarHeader={rulesToolbarHeader}
              toolbar={rulesToolbarActions}
              contextMenuItems={ruleContextMenu}
              emptyAction={
                <Button size="md" variant="primary" leadingIcon="plus" onClick={startNewRootRule}>
                  New rule
                </Button>
              }
            />
          ) : (
            <DataTable<AggregateRule>
              data={aggregateRows}
              columns={aggregateRuleColumns}
              rowKey={(r) => r.uid ?? r.name}
              loading={list.isPending}
              rowDisabled={ruleRowDisabled}
              contextMenuItems={aggregateContextMenu}
              selectable
              selectedKeys={aggregateSelected}
              onSelectionChange={setAggregateSelected}
              search={aggregateSearch.searchProp}
              toolbarHeader={aggregateToolbarHeader}
              toolbar={aggregateToolbarActions}
              emptyState={
                <EmptyState
                  icon="file-text"
                  title="No aggregate rules yet"
                  description="Aggregate rules group recurring alerts by a key field."
                  action={
                    <Button
                      size="md"
                      variant="primary"
                      leadingIcon="plus"
                      onClick={() => setCreating(true)}
                    >
                      New aggregate rule
                    </Button>
                  }
                />
              }
              renderExpanded={(row) => (
                <RowDetailPanel
                  row={row as unknown as Record<string, unknown>}
                  objectType="aggregaterule"
                  objectId={row.uid}
                />
              )}
              serverSort={{
                sortBy: orderby,
                order: asc ? "asc" : "desc",
                onChange: (next) =>
                  updateSearch({
                    orderby: next.sortBy,
                    asc: next.order === "asc",
                    page: 1,
                  }),
              }}
              serverPagination={{
                page,
                pageSize: PAGE_SIZE,
                total: list.data?.meta.total ?? 0,
                onChange: (next) => updateSearch({ page: next.page }),
              }}
              onRowOpen={(row) => {
                if (row.uid) updateSearch({ uid: row.uid });
              }}
            />
          )}
        </TabPanel>
      </Tabs>

      {detailUid !== undefined ? (
        <RuleEditor
          plugin={editorPlugin}
          uid={detailUid}
          onClose={() => updateSearch({ uid: undefined })}
        />
      ) : null}

      {creating ? (
        <RuleEditor
          plugin={editorPlugin}
          uid={undefined}
          onClose={() => {
            setCreating(false);
            setPendingInsertion(null);
          }}
          {...(pendingInsertion ? { insertion: pendingInsertion } : {})}
        />
      ) : null}
      <ConfirmDeleteDialog
        state={confirmDelete.state}
        onCancel={confirmDelete.cancel}
        onConfirm={() => void confirmDelete.confirm()}
      />
      <ConfirmDeleteDialog
        state={confirmDeleteRule.state}
        onCancel={confirmDeleteRule.cancel}
        onConfirm={() => void confirmDeleteRule.confirm()}
      />
    </div>
  );
}
