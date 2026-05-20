import { useCallback, useMemo, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "@/shared/ui/Button";
import { DataTable, type RowAction } from "@/shared/ui/DataTable";
import type { ContextMenuItem } from "@/shared/ui/DataTableContextMenu";
import { EmptyState } from "@/shared/ui/EmptyState";
import { RowDetailPanel } from "@/shared/ui/RowDetailPanel";
import { TabList, TabPanel, TabTrigger, Tabs } from "@/shared/ui/Tabs";
import { toast } from "@/shared/ui/toast/useToast";
import { useTableSearch } from "@/shared/hooks/useTableSearch";
import {
  buildResourceContextMenu,
  ConfirmDeleteDialog,
  useConfirmDelete,
} from "@/shared/ui/resourceContextMenu";
import { api as apiClient, ApiError } from "@/lib/api/client";
import { Records } from "@/features/alerts/api";
import { Snoozes } from "./api";
import { SnoozeEditor } from "./SnoozeEditor";
import { snoozeColumns, snoozeRowDisabled } from "./columns";
import { snoozeState, type SnoozeState } from "./state";
import type { Snooze } from "./types";
import styles from "./SnoozesPage.module.css";

type RetroApplyResponse = {
  matched: number;
  deleted?: number;
  tagged?: number;
  snooze: string;
};

type SnoozesSearch = {
  uid?: string | undefined;
  page?: number;
  orderby?: string;
  asc?: boolean;
  tab?: SnoozeState;
};

// TanStack Router's navigate types are locked to the registered route tree at
// build time. Casting through unknown avoids type errors when the route is
// locally constructed in tests and still works when fully registered.
type NavigateFn = (opts: {
  to: string;
  search: (prev: SnoozesSearch | undefined) => SnoozesSearch;
}) => Promise<void>;

const PAGE_SIZE = 50;
const TABS: { value: SnoozeState; label: string }[] = [
  { value: "active", label: "Active" },
  { value: "upcoming", label: "Upcoming" },
  { value: "expired", label: "Expired" },
];

export function SnoozesPage() {
  // useSearch with strict:false returns the validated search params; cast for local type.
  const search = useSearch({ strict: false }) as unknown as SnoozesSearch;
  const navigate = useNavigate();

  const page = search.page ?? 1;
  const orderby = search.orderby ?? "name";
  const asc = search.asc ?? true;
  const detailUid = search.uid;
  const tab: SnoozeState = search.tab ?? "active";
  const [creating, setCreating] = useState(false);

  const updateSearch = useCallback(
    (next: SnoozesSearch) => {
      void (navigate as unknown as NavigateFn)({
        to: "/web/snoozes",
        search: (prev: SnoozesSearch | undefined) => {
          const merged = { ...(prev ?? {}), ...next };
          // exactOptionalPropertyTypes: remove keys set to undefined rather than keeping them
          if (merged.uid === undefined) {
            const { uid: _uid, ...rest } = merged;
            return rest as SnoozesSearch;
          }
          return merged as SnoozesSearch;
        },
      });
    },
    [navigate],
  );

  const snoozeSearch = useTableSearch({
    collection: "snooze",
    placeholder: "name = … AND enabled = true",
    onFilterChange: () => {
      if (page !== 1) updateSearch({ page: 1 });
    },
  });

  // Snooze state (Active/Upcoming/Expired) is computed client-side from
  // time_constraints.datetime, so we have to fetch the full set to count
  // each tab and filter the visible rows. For a healthy ops setup the
  // total count is small (dozens, not thousands); if that changes we
  // push the predicate into a server-side `q` filter.
  const list = Snoozes.useList({
    limit: 1000,
    orderby,
    asc,
    ...(snoozeSearch.q ? { q: snoozeSearch.q } : {}),
  });
  const remove = Snoozes.useRemove();
  const qc = useQueryClient();
  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(new Set());

  const runRetroApply = useCallback(
    async (row: Snooze) => {
      if (!row.uid) return;
      try {
        const res = await apiClient<RetroApplyResponse>(
          "POST",
          `/snooze/${row.uid}/retro_apply`,
        );
        const verb = res.deleted ? "discarded" : "tagged";
        toast.success(`${res.matched} alerts ${verb} by ${row.name}`);
        void qc.invalidateQueries({ queryKey: Snoozes.queryKey.all });
        void qc.invalidateQueries({ queryKey: Records.queryKey.all });
      } catch (e) {
        toast.error(e instanceof ApiError ? e.detail : "Retro-apply failed");
      }
    },
    [qc],
  );

  const confirmDelete = useConfirmDelete<Snooze>({
    onDelete: (uid) => remove.mutateAsync(uid),
    noun: "snooze",
    onAfter: () => setSelectedKeys(new Set()),
  });

  const allSnoozes = useMemo(() => list.data?.data ?? [], [list.data]);
  const counts = useMemo(() => {
    const c: Record<SnoozeState, number> = { active: 0, upcoming: 0, expired: 0 };
    for (const s of allSnoozes) c[snoozeState(s)] += 1;
    return c;
  }, [allSnoozes]);
  const filtered = useMemo(
    () => allSnoozes.filter((s) => snoozeState(s) === tab),
    [allSnoozes, tab],
  );
  const paged = filtered.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE);

  const rowActions = useCallback(
    (row: Snooze): RowAction[] => {
      if (!row.uid) return [];
      return [
        {
          key: "retro-apply",
          label: row.discard
            ? "Retro-apply (delete matches)"
            : "Retro-apply (tag matches)",
          icon: "rotate-cw",
          onSelect: () => void runRetroApply(row),
        },
        {
          key: "edit",
          label: "Edit",
          icon: "edit",
          onSelect: () => updateSearch({ uid: row.uid! }),
        },
      ];
    },
    [updateSearch, runRetroApply],
  );

  const contextMenuItems = useCallback(
    (row: Snooze): ContextMenuItem[] =>
      buildResourceContextMenu(row, {
        onOpen: (r) => {
          if (r.uid) updateSearch({ uid: r.uid });
        },
        onDelete: (uid) => remove.mutateAsync(uid),
        requestDelete: (r) => confirmDelete.request([r]),
        extras: (r) => [
          {
            key: "retro-apply",
            label: r.discard ? "Retro-apply (delete matches)" : "Retro-apply (tag matches)",
            icon: "rotate-cw",
            onSelect: () => void runRetroApply(r),
          },
        ],
      }),
    [updateSearch, remove, confirmDelete, runRetroApply],
  );

  const bulkActions = useCallback(
    (rows: Snooze[]) => (
      <>
        <Button
          size="sm"
          variant="secondary"
          leadingIcon="rotate-cw"
          onClick={() => {
            void (async () => {
              for (const r of rows) await runRetroApply(r);
            })();
          }}
        >
          Retro-apply ({rows.length})
        </Button>
        <Button
          size="sm"
          variant="danger"
          leadingIcon="trash"
          onClick={() => confirmDelete.request(rows)}
        >
          Delete ({rows.length})
        </Button>
      </>
    ),
    [confirmDelete, runRetroApply],
  );

  // Toolbar header + actions: now rendered next to the SearchBar via the
  // DataTable's `toolbarHeader` / `toolbar` slots so every list page shares
  // the same horizontal chrome.
  const selectedSnoozeRows = useMemo(
    () => paged.filter((r) => selectedKeys.has(r.uid ?? r.name)),
    [paged, selectedKeys],
  );
  const snoozesToolbarHeader =
    selectedSnoozeRows.length > 0
      ? `${selectedSnoozeRows.length} selected`
      : `${filtered.length} ${tab} snoozes`;
  const snoozesToolbarActions =
    selectedSnoozeRows.length > 0 ? (
      bulkActions(selectedSnoozeRows)
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
        onValueChange={(v) => updateSearch({ tab: v as SnoozeState, page: 1 })}
      >
        <TabList>
          {TABS.map((t) => (
            <TabTrigger key={t.value} value={t.value}>
              {t.label} ({counts[t.value]})
            </TabTrigger>
          ))}
        </TabList>
        <TabPanel value={tab}>
          <DataTable<Snooze>
            data={paged}
            columns={snoozeColumns}
            rowKey={(r) => r.uid ?? r.name}
            rowDisabled={snoozeRowDisabled}
            rowActions={rowActions}
            contextMenuItems={contextMenuItems}
            selectable
            selectedKeys={selectedKeys}
            onSelectionChange={setSelectedKeys}
            loading={list.isPending}
            search={snoozeSearch.searchProp}
            toolbarHeader={snoozesToolbarHeader}
            toolbar={snoozesToolbarActions}
            emptyState={
              <EmptyState
                icon="file-text"
                title={`No ${tab} snoozes`}
                description="Snoozes suppress alerts matching a condition for a time window."
                action={
                  <Button
                    size="md"
                    variant="primary"
                    leadingIcon="plus"
                    onClick={() => setCreating(true)}
                  >
                    New snooze
                  </Button>
                }
              />
            }
            renderExpanded={(row) => (
              <RowDetailPanel
                row={row as unknown as Record<string, unknown>}
                objectType="snooze"
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
              total: filtered.length,
              onChange: (next) => updateSearch({ page: next.page }),
            }}
            onRowOpen={(row) => {
              if (row.uid) updateSearch({ uid: row.uid });
            }}
          />
        </TabPanel>
      </Tabs>
      {detailUid !== undefined ? (
        <SnoozeEditor uid={detailUid} onClose={() => updateSearch({ uid: undefined })} />
      ) : null}
      {creating ? <SnoozeEditor uid={undefined} onClose={() => setCreating(false)} /> : null}
      <ConfirmDeleteDialog
        state={confirmDelete.state}
        onCancel={confirmDelete.cancel}
        onConfirm={() => void confirmDelete.confirm()}
      />
    </div>
  );
}
