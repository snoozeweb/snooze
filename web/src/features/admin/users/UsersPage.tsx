import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useSearch } from "@tanstack/react-router";
import { Button } from "@/shared/ui/Button";
import { DataTable } from "@/shared/ui/DataTable";
import { EmptyState } from "@/shared/ui/EmptyState";
import { RowDetailPanel } from "@/shared/ui/RowDetailPanel";
import { TabList, TabPanel, TabTrigger, Tabs } from "@/shared/ui/Tabs";
import { useTableSearch } from "@/shared/hooks/useTableSearch";
import { useResourceListPage, type BaseListSearch } from "@/shared/hooks/useResourceListPage";
import { ConfirmDeleteDialog } from "@/shared/ui/resourceContextMenu";
import { fetchLoginConfig } from "@/features/auth/api";
import { encodeConditionQ } from "@/lib/condition/serialize";
import type { Condition } from "@/lib/condition/types";
import { Users } from "./api";
import { Roles } from "@/features/admin/roles/api";
import { UserEditor } from "./UserEditor";
import { makeUserColumns } from "./columns";
import type { User } from "./types";
import styles from "./UsersPage.module.css";

// A tab is either "all" or an auth-method name (local/ldap or an OIDC method
// such as "microsoft"). It maps directly to the `method` filter value.
type UserTab = string;

type UsersSearch = BaseListSearch & {
  tab?: UserTab;
};

const PAGE_SIZE = 50;

export function UsersPage() {
  // useSearch with strict:false returns the validated search params; cast for local type.
  const search = useSearch({ strict: false }) as unknown as UsersSearch;

  const page = search.page ?? 1;
  const orderby = search.orderby ?? "name";
  const asc = search.asc ?? true;
  const detailUid = search.uid;
  const tab: UserTab = search.tab ?? "all";
  const [creating, setCreating] = useState(false);

  const remove = Users.useRemove();
  const {
    updateSearch,
    selectedKeys,
    setSelectedKeys,
    confirmDelete,
    contextMenuItems,
    bulkActions,
  } = useResourceListPage<User, UsersSearch>({
    to: "/web/admin/users",
    remove,
    noun: "user",
  });

  // Login backends — drives LDAP tab visibility. Anonymous backend has no
  // user records of its own so we never show a tab for it.
  const backends = useQuery({
    queryKey: ["login-backends"],
    queryFn: fetchLoginConfig,
    staleTime: 5 * 60_000,
  });
  const ldapEnabled = backends.data?.backends.some((b) => b.name === "ldap") ?? false;
  // Redirect (OIDC/OAuth) backends — e.g. Microsoft 365 — each get their own
  // tab so admins can browse the SSO users provisioned on first login.
  const redirectBackends = useMemo(
    () => backends.data?.backends.filter((b) => b.kind === "redirect") ?? [],
    [backends.data],
  );

  // Roles + their Groups feed the group→role mapping. We index group value →
  // role names so the Roles column can show the roles a user effectively holds
  // via their auth-backend groups (an SSO admin carries no explicit `roles`).
  const rolesList = Roles.useList({ limit: 500 });
  const roleGroupIndex = useMemo(() => {
    const index = new Map<string, string[]>();
    for (const role of rolesList.data?.data ?? []) {
      for (const g of role.groups ?? []) {
        const arr = index.get(g);
        if (arr) arr.push(role.name);
        else index.set(g, [role.name]);
      }
    }
    return index;
  }, [rolesList.data]);
  const columns = useMemo(() => makeUserColumns(roleGroupIndex), [roleGroupIndex]);

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
    // Every non-"all" tab is an auth method; filter the list to that method.
    if (tab !== "all") {
      parts.push({ type: "EQUALS", field: "method", value: tab });
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

  const selectedUserRows = useMemo(
    () => (list.data?.data ?? []).filter((r) => selectedKeys.has(r.uid ?? r.name)),
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
      <Button size="sm" variant="primary" leadingIcon="plus" onClick={() => setCreating(true)}>
        New
      </Button>
    );

  return (
    <div className={styles.page}>
      <Tabs value={tab} onValueChange={(v) => updateSearch({ tab: v, page: 1 })}>
        <TabList>
          <TabTrigger value="all">All</TabTrigger>
          <TabTrigger value="local">Local</TabTrigger>
          {ldapEnabled ? <TabTrigger value="ldap">LDAP</TabTrigger> : null}
          {redirectBackends.map((b) => (
            <TabTrigger key={b.name} value={b.name}>
              {b.display_name || b.name}
            </TabTrigger>
          ))}
        </TabList>
        <TabPanel value={tab}>
          <DataTable<User>
            data={list.data?.data ?? []}
            columns={columns}
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
