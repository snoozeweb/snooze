import { useState } from "react";
import { useSearch } from "@tanstack/react-router";
import { Button } from "@/shared/ui/Button";
import { DataTable } from "@/shared/ui/DataTable";
import { EmptyState } from "@/shared/ui/EmptyState";
import { RowDetailPanel } from "@/shared/ui/RowDetailPanel";
import { useTableSearch } from "@/shared/hooks/useTableSearch";
import { useResourceListPage, type BaseListSearch } from "@/shared/hooks/useResourceListPage";
import { ConfirmDeleteDialog } from "@/shared/ui/resourceContextMenu";
import { Groups } from "./api";
import { GroupEditor } from "./GroupEditor";
import { groupColumns } from "./columns";
import type { Group } from "./types";
import styles from "./GroupsPage.module.css";

type GroupsSearch = BaseListSearch;

const PAGE_SIZE = 50;

export function GroupsPage() {
  // useSearch with strict:false returns the validated search params; cast for local type.
  const search = useSearch({ strict: false }) as unknown as GroupsSearch;

  const page = search.page ?? 1;
  const orderby = search.orderby ?? "name";
  const asc = search.asc ?? true;
  const detailUid = search.uid;
  const [creating, setCreating] = useState(false);

  const remove = Groups.useRemove();
  const {
    updateSearch,
    selectedKeys,
    setSelectedKeys,
    confirmDelete,
    contextMenuItems,
    bulkActions,
  } = useResourceListPage<Group, GroupsSearch>({
    to: "/web/admin/groups",
    remove,
    noun: "group",
  });

  const groupsSearch = useTableSearch({
    collection: "group",
    placeholder: "name = … AND members CONTAINS …",
    onFilterChange: () => {
      if (page !== 1) updateSearch({ page: 1 });
    },
  });

  const list = Groups.useList({
    offset: (page - 1) * PAGE_SIZE,
    limit: PAGE_SIZE,
    orderby,
    asc,
    ...(groupsSearch.q ? { q: groupsSearch.q } : {}),
  });

  return (
    <div className={styles.page}>
      <DataTable<Group>
        data={list.data?.data ?? []}
        columns={groupColumns}
        rowKey={(r) => r.uid ?? r.name}
        loading={list.isPending}
        contextMenuItems={contextMenuItems}
        selectable
        selectedKeys={selectedKeys}
        onSelectionChange={setSelectedKeys}
        bulkActions={bulkActions}
        toolbarHeader={`${list.data?.meta.total ?? 0} groups`}
        toolbar={
          <Button size="sm" variant="primary" leadingIcon="plus" onClick={() => setCreating(true)}>
            New
          </Button>
        }
        search={groupsSearch.searchProp}
        emptyState={
          <EmptyState
            icon="file-text"
            title="No groups yet"
            description="Groups bundle members for role assignment without editing each user."
            action={
              <Button
                size="md"
                variant="primary"
                leadingIcon="plus"
                onClick={() => setCreating(true)}
              >
                New group
              </Button>
            }
          />
        }
        renderExpanded={(row) => (
          <RowDetailPanel
            row={row as unknown as Record<string, unknown>}
            objectType="group"
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
        <GroupEditor uid={detailUid} onClose={() => updateSearch({ uid: undefined })} />
      ) : null}
      {creating ? <GroupEditor uid={undefined} onClose={() => setCreating(false)} /> : null}
      <ConfirmDeleteDialog
        state={confirmDelete.state}
        onCancel={confirmDelete.cancel}
        onConfirm={() => void confirmDelete.confirm()}
      />
    </div>
  );
}
