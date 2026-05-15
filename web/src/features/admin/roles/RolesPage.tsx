import { useCallback, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { Button } from "@/shared/ui/Button";
import { DataTable } from "@/shared/ui/DataTable";
import type { ContextMenuItem } from "@/shared/ui/DataTableContextMenu";
import { RowDetailPanel } from "@/shared/ui/RowDetailPanel";
import {
  buildResourceContextMenu,
  ConfirmDeleteDialog,
  useConfirmDelete,
} from "@/shared/ui/resourceContextMenu";
import { Roles } from "./api";
import { RoleEditor } from "./RoleEditor";
import { roleColumns } from "./columns";
import type { Role } from "./types";
import styles from "./RolesPage.module.css";

type RolesSearch = {
  uid?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
};

// TanStack Router's navigate types are locked to the registered route tree at
// build time. Casting through unknown avoids type errors when the route is
// locally constructed in tests and still works when fully registered.
type NavigateFn = (opts: {
  to: string;
  search: (prev: RolesSearch | undefined) => RolesSearch;
}) => Promise<void>;

const PAGE_SIZE = 50;

export function RolesPage() {
  // useSearch with strict:false returns the validated search params; cast for local type.
  const search = useSearch({ strict: false }) as unknown as RolesSearch;
  const navigate = useNavigate();

  const page = search.page ?? 1;
  const orderby = search.orderby ?? "name";
  const asc = search.asc ?? true;
  const detailUid = search.uid;
  const [creating, setCreating] = useState(false);

  const updateSearch = useCallback(
    (next: RolesSearch) => {
      void (navigate as unknown as NavigateFn)({
        to: "/web/admin/roles",
        search: (prev: RolesSearch | undefined) => {
          const merged = { ...(prev ?? {}), ...next };
          // exactOptionalPropertyTypes: remove keys set to undefined rather than keeping them
          if (merged.uid === undefined) {
            const { uid: _uid, ...rest } = merged; // eslint-disable-line @typescript-eslint/no-unused-vars
            return rest as RolesSearch;
          }
          return merged as RolesSearch;
        },
      });
    },
    [navigate],
  );

  const list = Roles.useList({ offset: (page - 1) * PAGE_SIZE, limit: PAGE_SIZE, orderby, asc });
  const remove = Roles.useRemove();
  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(new Set());
  const confirmDelete = useConfirmDelete<Role>({
    onDelete: (uid) => remove.mutateAsync(uid),
    noun: "role",
    onAfter: () => setSelectedKeys(new Set()),
  });
  const contextMenuItems = useCallback(
    (row: Role): ContextMenuItem[] =>
      buildResourceContextMenu(row, {
        onOpen: (r) => {
          if (r.uid) updateSearch({ uid: r.uid });
        },
        onDelete: (uid) => remove.mutateAsync(uid),
        requestDelete: (r) => confirmDelete.request([r]),
      }),
    [updateSearch, remove, confirmDelete],
  );
  const bulkActions = useCallback(
    (rows: Role[]) => (
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
      <div className={styles.topbar}>
        <span style={{ color: "var(--text-muted)", fontSize: "var(--text-sm)" }}>
          {list.data?.meta.total ?? 0} roles
        </span>
        <Button size="sm" variant="primary" leadingIcon="plus" onClick={() => setCreating(true)}>
          New
        </Button>
      </div>
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
        <RoleEditor uid={detailUid} onClose={() => updateSearch({})} />
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
