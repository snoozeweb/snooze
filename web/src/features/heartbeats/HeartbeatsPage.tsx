import { useCallback, useMemo, useState } from "react";
import { useSearch } from "@tanstack/react-router";
import { Button } from "@/shared/ui/Button";
import { DataTable } from "@/shared/ui/DataTable";
import { EmptyState } from "@/shared/ui/EmptyState";
import { RowDetailPanel } from "@/shared/ui/RowDetailPanel";
import { useTableSearch } from "@/shared/hooks/useTableSearch";
import { useResourceListPage, type BaseListSearch } from "@/shared/hooks/useResourceListPage";
import { ConfirmDeleteDialog } from "@/shared/ui/resourceContextMenu";
import { Heartbeats, useHeartbeatList } from "./api";
import { HeartbeatEditor } from "./HeartbeatEditor";
import { heartbeatColumns } from "./columns";
import type { Heartbeat, HeartbeatStatus } from "./types";
import styles from "./HeartbeatsPage.module.css";

type HeartbeatsSearch = BaseListSearch & {
  status?: HeartbeatStatus;
};

const PAGE_SIZE = 50;

const STATUS_FILTERS: { value: HeartbeatStatus | undefined; label: string }[] = [
  { value: undefined, label: "All" },
  { value: "ok", label: "OK" },
  { value: "slow", label: "Slow" },
  { value: "overdue", label: "Overdue" },
];

export function HeartbeatsPage() {
  const search = useSearch({ strict: false }) as unknown as HeartbeatsSearch;

  const page = search.page ?? 1;
  const orderby = search.orderby ?? "name";
  const asc = search.asc ?? true;
  const detailUid = search.uid;
  const statusFilter = search.status;
  const [creating, setCreating] = useState(false);

  const remove = Heartbeats.useRemove();

  const {
    updateSearch,
    selectedKeys,
    setSelectedKeys,
    confirmDelete,
    contextMenuItems,
    bulkActions,
  } = useResourceListPage<Heartbeat, HeartbeatsSearch>({
    to: "/web/heartbeats",
    remove,
    noun: "heartbeat",
  });

  const heartbeatSearch = useTableSearch({
    collection: "heartbeat",
    placeholder: "name = … OR status = overdue",
    onFilterChange: () => {
      if (page !== 1) updateSearch({ page: 1 });
    },
  });

  const list = useHeartbeatList({
    limit: PAGE_SIZE,
    offset: (page - 1) * PAGE_SIZE,
    orderby,
    asc,
    ...(heartbeatSearch.q ? { q: heartbeatSearch.q } : {}),
    ...(statusFilter !== undefined ? { status: statusFilter } : {}),
  });

  const rows = useMemo(() => list.data?.data ?? [], [list.data]);
  const total = list.data?.meta.total ?? 0;

  const selectedHeartbeatRows = useMemo(
    () => rows.filter((r) => selectedKeys.has(r.uid ?? r.name)),
    [rows, selectedKeys],
  );

  const toolbarHeader =
    selectedHeartbeatRows.length > 0
      ? `${selectedHeartbeatRows.length} selected`
      : `${total} heartbeats`;

  const toolbarActions =
    selectedHeartbeatRows.length > 0 ? (
      bulkActions(selectedHeartbeatRows)
    ) : (
      <Button size="sm" variant="primary" leadingIcon="plus" onClick={() => setCreating(true)}>
        New
      </Button>
    );

  const handleStatusFilter = useCallback(
    (value: HeartbeatStatus | undefined) => {
      const next: Partial<HeartbeatsSearch> = { page: 1 };
      if (value !== undefined) next.status = value;
      else delete next.status;
      updateSearch(next);
    },
    [updateSearch],
  );

  return (
    <div className={styles.page}>
      <div className={styles.filterBar} role="group" aria-label="Status filter">
        {STATUS_FILTERS.map((f) => (
          <Button
            key={f.label}
            size="sm"
            variant={statusFilter === f.value ? "primary" : "secondary"}
            onClick={() => handleStatusFilter(f.value)}
          >
            {f.label}
          </Button>
        ))}
      </div>
      <DataTable<Heartbeat>
        data={rows}
        columns={heartbeatColumns}
        rowKey={(r) => r.uid ?? r.name}
        contextMenuItems={contextMenuItems}
        selectable
        selectedKeys={selectedKeys}
        onSelectionChange={setSelectedKeys}
        loading={list.isPending}
        search={heartbeatSearch.searchProp}
        toolbarHeader={toolbarHeader}
        toolbar={toolbarActions}
        emptyState={
          <EmptyState
            icon="activity"
            title="No heartbeats yet"
            description="Create a heartbeat to monitor a cron job or recurring process."
            action={
              <Button
                size="md"
                variant="primary"
                leadingIcon="plus"
                onClick={() => setCreating(true)}
              >
                New heartbeat
              </Button>
            }
          />
        }
        renderExpanded={(row) => (
          <RowDetailPanel
            row={row as unknown as Record<string, unknown>}
            objectType="heartbeat"
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
          total,
          onChange: (next) => updateSearch({ page: next.page }),
        }}
        onRowOpen={(row) => {
          if (row.uid) updateSearch({ uid: row.uid });
        }}
      />
      {detailUid !== undefined ? (
        <HeartbeatEditor uid={detailUid} onClose={() => updateSearch({ uid: undefined })} />
      ) : null}
      {creating ? <HeartbeatEditor uid={undefined} onClose={() => setCreating(false)} /> : null}
      <ConfirmDeleteDialog
        state={confirmDelete.state}
        onCancel={confirmDelete.cancel}
        onConfirm={() => void confirmDelete.confirm()}
      />
    </div>
  );
}
