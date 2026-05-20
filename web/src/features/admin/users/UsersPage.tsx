import { useCallback, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { Button } from "@/shared/ui/Button";
import { DataTable } from "@/shared/ui/DataTable";
import type { ContextMenuItem } from "@/shared/ui/DataTableContextMenu";
import { EmptyState } from "@/shared/ui/EmptyState";
import { RowDetailPanel } from "@/shared/ui/RowDetailPanel";
import { TabList, TabPanel, TabTrigger, Tabs } from "@/shared/ui/Tabs";
import { useTableSearch } from "@/shared/hooks/useTableSearch";
import { fetchLoginBackends } from "@/features/auth/api";
import { encodeConditionQ } from "@/lib/condition/serialize";
import type { Condition } from "@/lib/condition/types";
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

type UserTab = "all" | "local" | "ldap";

type UsersSearch = {
  uid?: string | undefined;
  page?: number;
  orderby?: string;
  asc?: boolean;
  tab?: UserTab;
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
  const tab: UserTab = search.tab ?? "all";
  const [creating, setCreating] = useState(false);

  // Login backends — drives LDAP tab visibility. Anonymous backend has no
  // user records of its own so we never show a tab for it.
  const backends = useQuery({
    queryKey: ["login-backends"],
    queryFn: fetchLoginBackends,
    staleTime: 5 * 60_000,
  });
  const ldapEnabled = backends.data?.includes("ldap") ?? false;

  const updateSearch = useCallback(
    (next: UsersSearch) => {
      void (navigate as unknown as NavigateFn)({
        to: "/web/admin/users",
        search: (prev: UsersSearch | undefined) => {
          const merged = { ...(prev ?? {}), ...next };
          // exactOptionalPropertyTypes: remove keys set to undefined rather than keeping them
          if (merged.uid === undefined) {
            const { uid: _uid, ...rest } = merged;
            return rest as UsersSearch;
          }
          return merged as UsersSearch;
        },
      });
    },
    [navigate],
  );

  const userSearch = useTableSearch({
    collection: "user",
    placeholder: "name = … AND method = …",
    onFilterChange: () => {
      if (page !== 1) updateSearch({ page: 1 });
    },
  });

  // Combine the active tab's `method = …` preset with the SearchBar's
  // condition into a single ?q=. The "All" tab has no preset, so a clean
  // search input collapses to no filter at all (the request stays cacheable).
  const q = useMemo(() => {
    const parts: Condition[] = [];
    if (tab === "local") {
      parts.push({ type: "EQUALS", field: "method", value: "local" });
    } else if (tab === "ldap") {
      parts.push({ type: "EQUALS", field: "method", value: "ldap" });
    }
    if (
      userSearch.condition &&
      userSearch.condition.op !== "" &&
      userSearch.condition.op !== "ALWAYS_TRUE"
    ) {
      parts.push(userSearch.condition as unknown as Condition);
    }
    if (parts.length === 0) return undefined;
    const combined: Condition =
      parts.length === 1 ? (parts[0] as Condition) : { type: "AND", args: parts };
    return encodeConditionQ(combined);
  }, [tab, userSearch.condition]);

  const list = Users.useList({
    offset: (page - 1) * PAGE_SIZE,
    limit: PAGE_SIZE,
    orderby,
    asc,
    ...(q ? { q } : {}),
  });
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

  const selectedUserRows = useMemo(
    () =>
      (list.data?.data ?? []).filter((r) => selectedKeys.has(r.uid ?? r.name)),
    [list.data, selectedKeys],
  );
  // Toolbar pieces — rendered next to the SearchBar inside DataTable so the
  // surface matches every other list page.
  const usersToolbarHeader =
    selectedUserRows.length > 0
      ? `${selectedUserRows.length} selected`
      : `${list.data?.meta.total ?? 0} users`;
  const usersToolbarActions =
    selectedUserRows.length > 0 ? (
      bulkActions(selectedUserRows)
    ) : (
      <Button
        size="sm"
        variant="primary"
        leadingIcon="plus"
        onClick={() => setCreating(true)}
      >
        New
      </Button>
    );

  return (
    <div className={styles.page}>
      <Tabs
        value={tab}
        onValueChange={(v) => updateSearch({ tab: v as UserTab, page: 1 })}
      >
        <TabList>
          <TabTrigger value="all">All</TabTrigger>
          <TabTrigger value="local">Local</TabTrigger>
          {ldapEnabled ? <TabTrigger value="ldap">LDAP</TabTrigger> : null}
        </TabList>
        <TabPanel value={tab}>
      <DataTable<User>
        data={list.data?.data ?? []}
        columns={userColumns}
        rowKey={(r) => r.uid ?? r.name}
        loading={list.isPending}
        contextMenuItems={contextMenuItems}
        selectable
        selectedKeys={selectedKeys}
        onSelectionChange={setSelectedKeys}
        search={userSearch.searchProp}
        toolbarHeader={usersToolbarHeader}
        toolbar={usersToolbarActions}
        emptyState={
          <EmptyState
            icon="file-text"
            title="No users yet"
            description="Add a user to grant access to the Snooze UI."
            action={
              <Button
                size="md"
                variant="primary"
                leadingIcon="plus"
                onClick={() => setCreating(true)}
              >
                New user
              </Button>
            }
          />
        }
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
        </TabPanel>
      </Tabs>
      {detailUid !== undefined ? (
        <UserEditor uid={detailUid} onClose={() => updateSearch({ uid: undefined })} />
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
