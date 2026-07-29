import { useState } from "react";
import { useSearch } from "@tanstack/react-router";
import { Button } from "@/shared/ui/Button";
import { DataTable } from "@/shared/ui/DataTable";
import { EmptyState } from "@/shared/ui/EmptyState";
import { RowDetailPanel } from "@/shared/ui/RowDetailPanel";
import { useResourceListPage, type BaseListSearch } from "@/shared/hooks/useResourceListPage";
import { ConfirmDeleteDialog } from "@/shared/ui/resourceContextMenu";
import { TenantMatchRules } from "./api";
import { TenantMatchEditor } from "./TenantMatchEditor";
import { tenantMatchColumns } from "./columns";
import type { TenantMatchRule } from "./types";
import styles from "./TenantRoutingPage.module.css";

type TenantRoutingSearch = BaseListSearch;

const PAGE_SIZE = 50;

export function TenantRoutingPage() {
  const search = useSearch({ strict: false }) as unknown as TenantRoutingSearch;

  const page = search.page ?? 1;
  const orderby = search.orderby ?? "priority";
  const asc = search.asc ?? true;
  const detailUid = search.uid;
  const [creating, setCreating] = useState(false);

  const remove = TenantMatchRules.useRemove();
  const {
    updateSearch,
    selectedKeys,
    setSelectedKeys,
    confirmDelete,
    contextMenuItems,
    rowActions,
    bulkActions,
  } = useResourceListPage<TenantMatchRule, TenantRoutingSearch>({
    to: "/web/admin/tenant-routing",
    remove,
    noun: "rule",
  });

  const list = TenantMatchRules.useList({
    offset: (page - 1) * PAGE_SIZE,
    limit: PAGE_SIZE,
    orderby,
    asc,
  });

  return (
    <div className={styles.page}>
      <DataTable<TenantMatchRule>
        data={list.data?.data ?? []}
        columns={tenantMatchColumns}
        rowKey={(r) => r.uid ?? `${r.match_type}-${r.match}`}
        loading={list.isPending}
        contextMenuItems={contextMenuItems}
        rowActions={rowActions}
        selectable
        selectedKeys={selectedKeys}
        onSelectionChange={setSelectedKeys}
        bulkActions={bulkActions}
        toolbarHeader={`${list.data?.meta.total ?? 0} rules`}
        toolbar={
          <Button size="sm" variant="primary" leadingIcon="plus" onClick={() => setCreating(true)}>
            New rule
          </Button>
        }
        emptyState={
          <EmptyState
            icon="sliders"
            title="No routing rules yet"
            description="Routing rules map user attributes (group, domain, or login) to a target tenant."
            action={
              <Button
                size="md"
                variant="primary"
                leadingIcon="plus"
                onClick={() => setCreating(true)}
              >
                New rule
              </Button>
            }
          />
        }
        renderDetails={(row) => (
          <RowDetailPanel
            row={row as unknown as Record<string, unknown>}
            objectType="tenant_match"
            objectId={row.uid ?? `${row.match_type}-${row.match}`}
          />
        )}
        serverSort={{
          sortBy: orderby,
          order: asc ? "asc" : "desc",
          onChange: (next) =>
            updateSearch({ orderby: next.sortBy, asc: next.order === "asc", page: 1 }),
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
      {detailUid !== undefined ? (
        <TenantMatchEditor uid={detailUid} onClose={() => updateSearch({ uid: undefined })} />
      ) : null}
      {creating ? <TenantMatchEditor uid={undefined} onClose={() => setCreating(false)} /> : null}
      <ConfirmDeleteDialog
        state={confirmDelete.state}
        onCancel={confirmDelete.cancel}
        onConfirm={() => void confirmDelete.confirm()}
      />
    </div>
  );
}
