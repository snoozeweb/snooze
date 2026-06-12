import { useState } from "react";
import { useSearch } from "@tanstack/react-router";
import { Button } from "@/shared/ui/Button";
import { DataTable } from "@/shared/ui/DataTable";
import { EmptyState } from "@/shared/ui/EmptyState";
import { RowDetailPanel } from "@/shared/ui/RowDetailPanel";
import { useTableSearch } from "@/shared/hooks/useTableSearch";
import { useResourceListPage, type BaseListSearch } from "@/shared/hooks/useResourceListPage";
import { ConfirmDeleteDialog } from "@/shared/ui/resourceContextMenu";
import { Environments } from "./api";
import { EnvironmentEditor } from "./EnvironmentEditor";
import { environmentColumns } from "./columns";
import type { Environment } from "./types";
import styles from "./EnvironmentsPage.module.css";

type EnvironmentsSearch = BaseListSearch;

const PAGE_SIZE = 50;

export function EnvironmentsPage() {
  // useSearch with strict:false returns the validated search params; cast for local type.
  const search = useSearch({ strict: false }) as unknown as EnvironmentsSearch;

  const page = search.page ?? 1;
  const orderby = search.orderby ?? "name";
  const asc = search.asc ?? true;
  const detailUid = search.uid;
  const [creating, setCreating] = useState(false);

  const remove = Environments.useRemove();
  const {
    updateSearch,
    selectedKeys,
    setSelectedKeys,
    confirmDelete,
    contextMenuItems,
    bulkActions,
  } = useResourceListPage<Environment, EnvironmentsSearch>({
    to: "/web/admin/environments",
    remove,
    noun: "environment",
  });

  const envSearch = useTableSearch({
    collection: "environment",
    placeholder: "name = …",
    onFilterChange: () => {
      if (page !== 1) updateSearch({ page: 1 });
    },
  });

  const list = Environments.useList({
    offset: (page - 1) * PAGE_SIZE,
    limit: PAGE_SIZE,
    orderby,
    asc,
    ...(envSearch.q ? { q: envSearch.q } : {}),
  });

  return (
    <div className={styles.page}>
      <DataTable<Environment>
        data={list.data?.data ?? []}
        columns={environmentColumns}
        rowKey={(r) => r.uid ?? r.name}
        loading={list.isPending}
        contextMenuItems={contextMenuItems}
        selectable
        selectedKeys={selectedKeys}
        onSelectionChange={setSelectedKeys}
        bulkActions={bulkActions}
        toolbarHeader={`${list.data?.meta.total ?? 0} environments`}
        toolbar={
          <Button size="sm" variant="primary" leadingIcon="plus" onClick={() => setCreating(true)}>
            New
          </Button>
        }
        search={envSearch.searchProp}
        emptyState={
          <EmptyState
            icon="file-text"
            title="No environments yet"
            description="Environment tags categorise hosts (prod, staging, …)."
            action={
              <Button
                size="md"
                variant="primary"
                leadingIcon="plus"
                onClick={() => setCreating(true)}
              >
                New environment
              </Button>
            }
          />
        }
        renderExpanded={(row) => (
          <RowDetailPanel
            row={row as unknown as Record<string, unknown>}
            objectType="environment"
            objectId={row.uid}
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
        <EnvironmentEditor uid={detailUid} onClose={() => updateSearch({ uid: undefined })} />
      ) : null}
      {creating ? <EnvironmentEditor uid={undefined} onClose={() => setCreating(false)} /> : null}
      <ConfirmDeleteDialog
        state={confirmDelete.state}
        onCancel={confirmDelete.cancel}
        onConfirm={() => void confirmDelete.confirm()}
      />
    </div>
  );
}
