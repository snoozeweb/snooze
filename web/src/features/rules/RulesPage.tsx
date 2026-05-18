import { useCallback, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { Button } from "@/shared/ui/Button";
import { DataTable } from "@/shared/ui/DataTable";
import type { ContextMenuItem } from "@/shared/ui/DataTableContextMenu";
import { RowDetailPanel } from "@/shared/ui/RowDetailPanel";
import { TabList, TabPanel, TabTrigger, Tabs } from "@/shared/ui/Tabs";
import {
  buildResourceContextMenu,
  ConfirmDeleteDialog,
  useConfirmDelete,
} from "@/shared/ui/resourceContextMenu";
import { AggregateRules, Rules } from "./api";
import { RuleEditor } from "./RuleEditor";
import { RulesTreeTable } from "./RulesTreeTable";
import { aggregateRuleColumns } from "./columns";
import { ruleRowDisabled } from "./ruleUtils";
import type { AggregateRule } from "./types";
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
  // Rules tab: load the full set (limit=1000) so the tree component can
  // build the parent/child hierarchy client-side without juggling
  // pagination across levels. Aggregates tab keeps the paginated table.
  const isTree = tab === "rules";
  const list = resource.useList(
    isTree
      ? { limit: 1000, orderby: "tree_order", asc: true }
      : { offset: (page - 1) * PAGE_SIZE, limit: PAGE_SIZE, orderby, asc },
  );

  const editorPlugin: "rule" | "aggregaterule" = tab === "rules" ? "rule" : "aggregaterule";

  const removeAggregate = AggregateRules.useRemove();
  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(new Set());
  const confirmDelete = useConfirmDelete<AggregateRule>({
    onDelete: (uid) => removeAggregate.mutateAsync(uid),
    noun: "aggregate rule",
    onAfter: () => setSelectedKeys(new Set()),
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
            <>
              <div className={styles.topbar}>
                <span style={{ color: "var(--text-muted)", fontSize: "var(--text-sm)" }}>
                  {list.data?.meta.total ?? 0} rules
                </span>
                <Button
                  size="sm"
                  variant="primary"
                  leadingIcon="plus"
                  onClick={() => setCreating(true)}
                >
                  New
                </Button>
              </div>
              <RulesTreeTable
                rules={list.data?.data ?? []}
                onRowOpen={(row) => {
                  if (row.uid) updateSearch({ uid: row.uid });
                }}
              />
            </>
          ) : (
            <DataTable<AggregateRule>
              data={(list.data?.data ?? []) as AggregateRule[]}
              columns={aggregateRuleColumns}
              rowKey={(r) => r.uid ?? r.name}
              loading={list.isPending}
              rowDisabled={ruleRowDisabled}
              contextMenuItems={aggregateContextMenu}
              selectable
              selectedKeys={selectedKeys}
              onSelectionChange={setSelectedKeys}
              bulkActions={aggregateBulkActions}
              toolbarHeader={`${list.data?.meta.total ?? 0} aggregate rules`}
              toolbar={
                <Button
                  size="sm"
                  variant="primary"
                  leadingIcon="plus"
                  onClick={() => setCreating(true)}
                >
                  New
                </Button>
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
        <RuleEditor plugin={editorPlugin} uid={undefined} onClose={() => setCreating(false)} />
      ) : null}
      <ConfirmDeleteDialog
        state={confirmDelete.state}
        onCancel={confirmDelete.cancel}
        onConfirm={() => void confirmDelete.confirm()}
      />
    </div>
  );
}
