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
import { Users } from "./api";
import { UserEditor } from "./UserEditor";
import { userColumns } from "./columns";
import type { User } from "./types";
import styles from "./UsersPage.module.css";

type UsersSearch = {
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
  search: (prev: UsersSearch | undefined) => UsersSearch;
}) => Promise<void>;

const PAGE_SIZE = 50;

export function UsersPage() {
  // useSearch with strict:false returns the validated search params; cast for local type.
  const search = useSearch({ strict: false }) as unknown as UsersSearch;
  const navigate = useNavigate();

  const page = search.page ?? 1;
  const orderby = search.orderby ?? "name";
  const asc = search.asc ?? true;
  const detailUid = search.uid;
  const [creating, setCreating] = useState(false);

  const updateSearch = useCallback(
    (next: UsersSearch) => {
      void (navigate as unknown as NavigateFn)({
        to: "/web/admin/users",
        search: (prev: UsersSearch | undefined) => {
          const merged = { ...(prev ?? {}), ...next };
          // exactOptionalPropertyTypes: remove keys set to undefined rather than keeping them
          if (merged.uid === undefined) {
            const { uid: _uid, ...rest } = merged; // eslint-disable-line @typescript-eslint/no-unused-vars
            return rest as UsersSearch;
          }
          return merged as UsersSearch;
        },
      });
    },
    [navigate],
  );

  const list = Users.useList({ offset: (page - 1) * PAGE_SIZE, limit: PAGE_SIZE, orderby, asc });
  const remove = Users.useRemove();
  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(new Set());
  const confirmDelete = useConfirmDelete<User>({
    onDelete: (uid) => remove.mutateAsync(uid),
    noun: "user",
    onAfter: () => setSelectedKeys(new Set()),
  });
  const contextMenuItems = useCallback(
    (row: User): ContextMenuItem[] =>
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
    (rows: User[]) => (
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
          {list.data?.meta.total ?? 0} users
        </span>
        <Button size="sm" variant="primary" leadingIcon="plus" onClick={() => setCreating(true)}>
          New
        </Button>
      </div>
      <DataTable<User>
        data={list.data?.data ?? []}
        columns={userColumns}
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
            objectType="user"
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
        <UserEditor uid={detailUid} onClose={() => updateSearch({})} />
      ) : null}
      {creating ? <UserEditor uid={undefined} onClose={() => setCreating(false)} /> : null}
      <ConfirmDeleteDialog
        state={confirmDelete.state}
        onCancel={confirmDelete.cancel}
        onConfirm={() => void confirmDelete.confirm()}
      />
    </div>
  );
}
