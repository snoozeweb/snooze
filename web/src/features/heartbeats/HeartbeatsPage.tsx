import { useMemo, useState } from "react";
import { useSearch } from "@tanstack/react-router";
import { Button } from "@/shared/ui/Button";
import { DataTable } from "@/shared/ui/DataTable";
import { EmptyState } from "@/shared/ui/EmptyState";
import { RowDetailPanel } from "@/shared/ui/RowDetailPanel";
import { TabList, TabPanel, TabTrigger, Tabs } from "@/shared/ui/Tabs";
import { DocsLink } from "@/shared/ui/DocsLink";
import { useTableSearch } from "@/shared/hooks/useTableSearch";
import { useResourceListPage, type BaseListSearch } from "@/shared/hooks/useResourceListPage";
import { ConfirmDeleteDialog } from "@/shared/ui/resourceContextMenu";
import { Heartbeats, useHeartbeatList } from "./api";
import { HeartbeatEditor } from "./HeartbeatEditor";
import { heartbeatColumns } from "./columns";
import type { Heartbeat, HeartbeatStatus } from "./types";
import styles from "./HeartbeatsPage.module.css";

type HeartbeatsSearch = BaseListSearch & {
  // `| undefined` (not just optional) so `updateSearch({ status: undefined })`
  // typechecks under exactOptionalPropertyTypes and actually clears the filter.
  status?: HeartbeatStatus | undefined;
};

const PAGE_SIZE = 50;

// The status filter is a tab strip — same menu affordance (accent underline on
// the active item) as every other list page, instead of a row of buttons. Each
// tab maps to the `status` query the API accepts; the "all" tab clears it.
const STATUS_TABS: { value: string; status: HeartbeatStatus | undefined; label: string }[] = [
  { value: "all", status: undefined, label: "All" },
  { value: "ok", status: "ok", label: "OK" },
  { value: "slow", status: "slow", label: "Slow" },
  { value: "overdue", status: "overdue", label: "Overdue" },
];

export function HeartbeatsPage() {
  const search = useSearch({ strict: false }) as unknown as HeartbeatsSearch;

  const page = search.page ?? 1;
  const orderby = search.orderby ?? "name";
  const asc = search.asc ?? true;
  const detailUid = search.uid;
  const statusFilter = search.status;
  const activeTab = statusFilter ?? "all";
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

  const handleStatusTab = (value: string) => {
    const next = STATUS_TABS.find((t) => t.value === value);
    // Passing `status: undefined` for "All" is what actually clears the filter:
    // updateSearch drops any key set to undefined. The previous handler set no
    // `status` key at all, so the merge kept the old value and "All" never took.
    updateSearch({ page: 1, status: next?.status });
  };

  const toolbarActions = (
    <Button size="sm" variant="primary" leadingIcon="plus" onClick={() => setCreating(true)}>
      New
    </Button>
  );

  return (
    <div className={styles.page}>
      {/* Self-explaining concept strip + docs deep-link, matching Notifications
          — the heartbeat model (a job that must check in, or an alert fires) is
          non-obvious, so it stays visible instead of only in the empty state. */}
      <p className={styles.conceptStrip}>
        <strong>Heartbeats</strong> are dead-man&apos;s switches — register a job that should check
        in on a schedule, and Snooze raises an alert when its ping stops arriving.{" "}
        <DocsLink slug="general/integrations/heartbeat" />
      </p>
      <Tabs value={activeTab} onValueChange={handleStatusTab}>
        <TabList>
          {STATUS_TABS.map((t) => (
            <TabTrigger key={t.value} value={t.value}>
              {t.label}
            </TabTrigger>
          ))}
        </TabList>
        <TabPanel value={activeTab}>
          <DataTable<Heartbeat>
            data={rows}
            columns={heartbeatColumns}
            rowKey={(r) => r.uid ?? r.name}
            contextMenuItems={contextMenuItems}
            selectable
            selectedKeys={selectedKeys}
            onSelectionChange={setSelectedKeys}
            bulkActions={bulkActions}
            loading={list.isPending}
            search={heartbeatSearch.searchProp}
            toolbarHeader={`${total} heartbeats`}
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
        </TabPanel>
      </Tabs>
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
