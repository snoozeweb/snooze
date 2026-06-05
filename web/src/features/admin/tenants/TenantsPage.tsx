import { useCallback, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { Button } from "@/shared/ui/Button";
import { DataTable } from "@/shared/ui/DataTable";
import type { ContextMenuItem } from "@/shared/ui/DataTableContextMenu";
import { EmptyState } from "@/shared/ui/EmptyState";
import { RowDetailPanel } from "@/shared/ui/RowDetailPanel";
import {
  buildResourceContextMenu,
  ConfirmDeleteDialog,
  useConfirmDelete,
} from "@/shared/ui/resourceContextMenu";
import { Tenants } from "./api";
import { TenantEditor } from "./TenantEditor";
import { tenantColumns } from "./columns";
import type { Tenant } from "./types";
import styles from "./TenantsPage.module.css";

type TenantsSearch = {
  uid?: string | undefined;
  page?: number;
  orderby?: string;
  asc?: boolean;
};

type NavigateFn = (opts: {
  to: string;
  search: (prev: TenantsSearch | undefined) => TenantsSearch;
}) => Promise<void>;

const PAGE_SIZE = 50;

export function TenantsPage() {
  const search = useSearch({ strict: false }) as unknown as TenantsSearch;
  const navigate = useNavigate();

  const page = search.page ?? 1;
  const orderby = search.orderby ?? "id";
  const asc = search.asc ?? true;
  const detailUid = search.uid;
  const [creating, setCreating] = useState(false);

  const updateSearch = useCallback(
    (next: TenantsSearch) => {
      void (navigate as unknown as NavigateFn)({
        to: "/web/admin/tenants",
        search: (prev: TenantsSearch | undefined) => {
          const merged = { ...(prev ?? {}), ...next };
          if (merged.uid === undefined) {
            const { uid: _uid, ...rest } = merged;
            return rest as TenantsSearch;
          }
          return merged as TenantsSearch;
        },
      });
    },
    [navigate],
  );

  const list = Tenants.useList({
    offset: (page - 1) * PAGE_SIZE,
    limit: PAGE_SIZE,
    orderby,
    asc,
  });
  const remove = Tenants.useRemove();
  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(new Set());
  const confirmDelete = useConfirmDelete<Tenant & { uid?: string }>({
    onDelete: (id) => remove.mutateAsync(id),
    noun: "tenant",
    onAfter: () => setSelectedKeys(new Set()),
  });
  const contextMenuItems = useCallback(
    (row: Tenant): ContextMenuItem[] =>
      buildResourceContextMenu(
        { ...row, uid: row.id } as Tenant & { uid?: string },
        {
          onOpen: (r) => {
            const uid = (r as Tenant & { uid?: string }).uid ?? row.id;
            if (uid) updateSearch({ uid });
          },
          onDelete: (uid) => remove.mutateAsync(uid),
          requestDelete: (r) =>
            confirmDelete.request([{ ...r, uid: (r as Tenant & { uid?: string }).uid ?? row.id }]),
        },
      ),
    [updateSearch, remove, confirmDelete],
  );
  const bulkActions = useCallback(
    (rows: Tenant[]) => (
      <Button
        size="sm"
        variant="danger"
        leadingIcon="trash"
        onClick={() => confirmDelete.request(rows.map((r) => ({ ...r, uid: r.id })))}
      >
        Delete ({rows.length})
      </Button>
    ),
    [confirmDelete],
  );

  return (
    <div className={styles.page}>
      <DataTable<Tenant>
        data={list.data?.data ?? []}
        columns={tenantColumns}
        rowKey={(r) => r.id}
        loading={list.isPending}
        contextMenuItems={contextMenuItems}
        selectable
        selectedKeys={selectedKeys}
        onSelectionChange={setSelectedKeys}
        bulkActions={bulkActions}
        toolbarHeader={`${list.data?.meta.total ?? 0} tenants`}
        toolbar={
          <Button size="sm" variant="primary" leadingIcon="plus" onClick={() => setCreating(true)}>
            New
          </Button>
        }
        emptyState={
          <EmptyState
            icon="layers"
            title="No tenants yet"
            description="Tenants partition data and users into isolated organizations."
            action={
              <Button
                size="md"
                variant="primary"
                leadingIcon="plus"
                onClick={() => setCreating(true)}
              >
                New tenant
              </Button>
            }
          />
        }
        renderExpanded={(row) => (
          <RowDetailPanel
            row={row as unknown as Record<string, unknown>}
            objectType="tenant"
            objectId={row.id}
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
        onRowOpen={(row) => updateSearch({ uid: row.id })}
      />
      {detailUid !== undefined ? (
        <TenantEditor id={detailUid} onClose={() => updateSearch({ uid: undefined })} />
      ) : null}
      {creating ? <TenantEditor id={undefined} onClose={() => setCreating(false)} /> : null}
      <ConfirmDeleteDialog
        state={confirmDelete.state}
        onCancel={confirmDelete.cancel}
        onConfirm={() => void confirmDelete.confirm()}
      />
    </div>
  );
}
