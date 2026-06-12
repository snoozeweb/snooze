import { useState } from "react";
import { useSearch } from "@tanstack/react-router";
import { Button } from "@/shared/ui/Button";
import { DataTable } from "@/shared/ui/DataTable";
import { EmptyState } from "@/shared/ui/EmptyState";
import { RowDetailPanel } from "@/shared/ui/RowDetailPanel";
import { useTableSearch } from "@/shared/hooks/useTableSearch";
import { useResourceListPage, type BaseListSearch } from "@/shared/hooks/useResourceListPage";
import { ConfirmDeleteDialog } from "@/shared/ui/resourceContextMenu";
import { Roles } from "./api";
import { RoleEditor } from "./RoleEditor";
import { roleColumns } from "./columns";
import type { Role } from "./types";
import styles from "./RolesPage.module.css";

type RolesSearch = BaseListSearch;

const PAGE_SIZE = 50;

export function RolesPage() {
  // useSearch with strict:false returns the validated search params; cast for local type.
  const search = useSearch({ strict: false }) as unknown as RolesSearch;

  const page = search.page ?? 1;
  const orderby = search.orderby ?? "name";
  const asc = search.asc ?? true;
  const detailUid = search.uid;
  const [creating, setCreating] = useState(false);

  const remove = Roles.useRemove();
  const {
    updateSearch,
    selectedKeys,
    setSelectedKeys,
    confirmDelete,
    contextMenuItems,
    bulkActions,
  } = useResourceListPage<Role, RolesSearch>({
    to: "/web/admin/roles",
    remove,
    noun: "role",
  });

  const rolesSearch = useTableSearch({
    collection: "role",
    placeholder: "name = … AND permissions CONTAINS …",
    onFilterChange: () => {
      if (page !== 1) updateSearch({ page: 1 });
    },
  });

  const list = Roles.useList({
    offset: (page - 1) * PAGE_SIZE,
    limit: PAGE_SIZE,
    orderby,
    asc,
    ...(rolesSearch.q ? { q: rolesSearch.q } : {}),
  });

  return (
    <div className={styles.page}>
      <DataTable<Role>
        data={list.data?.data ?? []}
        columns={roleColumns}
        rowKey={(r) => r.uid ?? r.name}
        loading={list.isPending}
        contextMenuItems={contextMenuItems}
        selectable
        selectedKeys={selectedKeys}
        onSelectionChange={setSelectedKeys}
        bulkActions={bulkActions}
        toolbarHeader={`${list.data?.meta.total ?? 0} roles`}
        toolbar={
          <Button size="sm" variant="primary" leadingIcon="plus" onClick={() => setCreating(true)}>
            New
          </Button>
        }
        search={rolesSearch.searchProp}
        emptyState={
          <EmptyState
            icon="file-text"
            title="No roles yet"
            description="Roles bundle permissions for one or more users."
            action={
              <Button
                size="md"
                variant="primary"
                leadingIcon="plus"
                onClick={() => setCreating(true)}
              >
                New role
              </Button>
            }
          />
        }
        renderExpanded={(row) => (
          <RowDetailPanel
            row={row as unknown as Record<string, unknown>}
            objectType="role"
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
        <RoleEditor uid={detailUid} onClose={() => updateSearch({ uid: undefined })} />
      ) : null}
      {creating ? <RoleEditor uid={undefined} onClose={() => setCreating(false)} /> : null}
      <ConfirmDeleteDialog
        state={confirmDelete.state}
        onCancel={confirmDelete.cancel}
        onConfirm={() => void confirmDelete.confirm()}
      />
    </div>
  );
}
