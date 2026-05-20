import { useCallback, useMemo, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
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
  // Selection state is held per-tab so switching between Rules and
  // Aggregates doesn't accidentally cross-contaminate.
  const [ruleSelected, setRuleSelected] = useState<Set<string>>(new Set());
  const [aggregateSelected, setAggregateSelected] = useState<Set<string>>(new Set());
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
  const headerActions = isTree
    ? selectedRuleRows.length > 0
      ? (
        <>
          <span className={styles.selectionCount}>{selectedRuleRows.length} selected</span>
          <Button
            size="sm"
            variant="danger"
            leadingIcon="trash"
            onClick={() => confirmDeleteRule.request(selectedRuleRows)}
          >
            Delete ({selectedRuleRows.length})
          </Button>
        </>
      )
      : (
        // Rules are an ordered tree, so we deliberately omit the global
        // "+ New" button — every new rule needs an explicit anchor and
        // direction. The empty-state CTA handles the "no rules yet" case;
        // the per-row "+ Add" menu handles every populated case.
        <span className={styles.headerCount}>{list.data?.meta.total ?? 0} rules</span>
      )
    : selectedAggregateRows.length > 0
    ? (
      <>
        <span className={styles.selectionCount}>{selectedAggregateRows.length} selected</span>
        {aggregateBulkActions(selectedAggregateRows)}
      </>
    )
    : (
      <>
        <span className={styles.headerCount}>{list.data?.meta.total ?? 0} aggregate rules</span>
        <Button
          size="sm"
          variant="primary"
          leadingIcon="plus"
          onClick={() => setCreating(true)}
        >
          New
        </Button>
      </>
    );

  return (
    <div className={styles.page}>
      <Tabs
        value={tab}
        onValueChange={(v) => updateSearch({ tab: v as "rules" | "aggregates", page: 1 })}
      >
        <TabList rightSlot={headerActions}>
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
              emptyAction={
                <Button
                  size="md"
                  variant="primary"
                  leadingIcon="plus"
                  onClick={() => {
                    setPendingInsertion({
                      parents: [],
                      tree_order: 0,
                      siblingPatches: [],
                    });
                    setCreating(true);
                  }}
                >
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
        <RuleEditor plugin={editorPlugin} uid={detailUid} onClose={() => updateSearch({ uid: undefined })} />
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
