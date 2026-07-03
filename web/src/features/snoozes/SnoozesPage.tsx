import { useCallback, useMemo, useState } from "react";
import { useSearch } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "@/shared/ui/Button";
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogTitle } from "@/shared/ui/Dialog";
import { DataTable, type RowAction } from "@/shared/ui/DataTable";
import type { ContextMenuItem } from "@/shared/ui/DataTableContextMenu";
import { EmptyState } from "@/shared/ui/EmptyState";
import { RowDetailPanel } from "@/shared/ui/RowDetailPanel";
import { TabList, TabPanel, TabTrigger, Tabs } from "@/shared/ui/Tabs";
import { toast } from "@/shared/ui/toast/useToast";
import { useTableSearch } from "@/shared/hooks/useTableSearch";
import { useResourceListPage, type BaseListSearch } from "@/shared/hooks/useResourceListPage";
import { ConfirmDeleteDialog } from "@/shared/ui/resourceContextMenu";
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

type SnoozesSearch = BaseListSearch & {
  tab?: SnoozeState;
};

const PAGE_SIZE = 50;
const TABS: { value: SnoozeState; label: string }[] = [
  { value: "active", label: "Active" },
  { value: "upcoming", label: "Upcoming" },
  { value: "expired", label: "Expired" },
];

export function SnoozesPage() {
  // useSearch with strict:false returns the validated search params; cast for local type.
  const search = useSearch({ strict: false }) as unknown as SnoozesSearch;

  const page = search.page ?? 1;
  const orderby = search.orderby ?? "name";
  const asc = search.asc ?? true;
  const detailUid = search.uid;
  const tab: SnoozeState = search.tab ?? "active";
  const [creating, setCreating] = useState(false);

  const remove = Snoozes.useRemove();
  const qc = useQueryClient();

  // A discard snooze's retro-apply permanently DELETES every matching alert
  // (backend hard-deletes when discard=true), so it goes behind a confirm.
  // Tag-mode retro-apply is non-destructive and runs immediately.
  const [retroConfirm, setRetroConfirm] = useState<Snooze[] | null>(null);
  const [retroBusy, setRetroBusy] = useState(false);

  const doRetroApply = useCallback(
    async (rows: Snooze[]) => {
      const targets = rows.filter((r) => r.uid);
      if (targets.length === 0) return;
      const results = await Promise.allSettled(
        targets.map((r) => apiClient<RetroApplyResponse>("POST", `/snooze/${r.uid}/retro_apply`)),
      );
      if (targets.length === 1) {
        const res = results[0];
        if (res && res.status === "fulfilled") {
          const verb = res.value.deleted ? "discarded" : "tagged";
          toast.success(`${res.value.matched} alerts ${verb} by ${targets[0]!.name}`);
        } else {
          const reason =
            res && res.status === "rejected" && res.reason instanceof ApiError
              ? res.reason.detail
              : "Retro-apply failed";
          toast.error(reason);
        }
      } else {
        const ok = results.filter((r) => r.status === "fulfilled").length;
        const failed = results.length - ok;
        if (failed === 0) toast.success(`Retro-applied ${ok} snooze${ok === 1 ? "" : "s"}`);
        else if (ok === 0)
          toast.error(`Retro-apply failed for all ${failed} snooze${failed === 1 ? "" : "s"}`);
        else toast.error(`Retro-apply: ${ok} succeeded, ${failed} failed`);
      }
      void qc.invalidateQueries({ queryKey: Snoozes.queryKey.all });
      void qc.invalidateQueries({ queryKey: Records.queryKey.all });
    },
    [qc],
  );

  // Confirm first when any target is a discard snooze; otherwise run now.
  const requestRetroApply = useCallback(
    (rows: Snooze[]) => {
      if (rows.some((r) => r.discard)) setRetroConfirm(rows);
      else void doRetroApply(rows);
    },
    [doRetroApply],
  );

  // Inject a retro-apply item into the standard resource context menu.
  const contextMenuExtras = useCallback(
    (r: Snooze): ContextMenuItem[] => [
      {
        key: "retro-apply",
        label: r.discard ? "Retro-apply (delete matches)" : "Retro-apply (tag matches)",
        icon: r.discard ? "trash" : "rotate-cw",
        ...(r.discard ? { danger: true } : {}),
        onSelect: () => requestRetroApply([r]),
      },
    ],
    [requestRetroApply],
  );

  const { updateSearch, selectedKeys, setSelectedKeys, confirmDelete, contextMenuItems } =
    useResourceListPage<Snooze, SnoozesSearch>({
      to: "/web/snoozes",
      remove,
      noun: "snooze",
      contextMenuExtras,
    });

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

  // Visible kebab mirrors the right-click menu (Copy JSON / Copy YAML /
  // retro-apply / Delete via contextMenuItems) plus Edit up front — Snoozes
  // rows expand inline on click instead of opening the editor, so Edit needs
  // its own entry here. Without this, Copy/Delete would hide behind the
  // touch-unreachable context menu the same way c475acbe fixed elsewhere.
  const rowActions = useCallback(
    (row: Snooze): RowAction[] => {
      if (!row.uid) return [];
      return [
        {
          key: "edit",
          label: "Edit",
          icon: "edit",
          onSelect: () => updateSearch({ uid: row.uid! }),
        },
        ...contextMenuItems(row),
      ];
    },
    [updateSearch, contextMenuItems],
  );

  const bulkActions = useCallback(
    (rows: Snooze[]) => (
      <>
        <Button
          size="sm"
          variant="secondary"
          leadingIcon="rotate-cw"
          onClick={() => requestRetroApply(rows)}
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
    [confirmDelete, requestRetroApply],
  );

  // Toolbar header + actions: now rendered next to the SearchBar via the
  // DataTable's `toolbarHeader` / `toolbar` slots so every list page shares
  // the same horizontal chrome. `bulkActions` goes straight to DataTable's
  // own `bulkActions` prop — that's what switches the toolbar chip to its
  // amber "selected" treatment, which building the text here ourselves
  // would skip.
  const snoozesToolbarHeader = `${filtered.length} ${tab} snoozes`;
  const snoozesToolbarActions = (
    <Button size="sm" variant="primary" leadingIcon="plus" onClick={() => setCreating(true)}>
      New
    </Button>
  );

  return (
    <div className={styles.page}>
      <Tabs value={tab} onValueChange={(v) => updateSearch({ tab: v as SnoozeState, page: 1 })}>
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
            bulkActions={bulkActions}
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
      <Dialog
        open={retroConfirm !== null}
        onOpenChange={(o) => {
          if (!o) setRetroConfirm(null);
        }}
      >
        <DialogContent>
          <DialogTitle>Delete matching alerts?</DialogTitle>
          <DialogBody>
            {(() => {
              const rows = retroConfirm ?? [];
              const discarders = rows.filter((r) => r.discard);
              if (rows.length === 1) {
                return `"${rows[0]!.name}" is a discard snooze — retro-applying it permanently deletes every alert that currently matches its condition. This cannot be undone.`;
              }
              return `${discarders.length} of the ${rows.length} selected snoozes discard matches — retro-applying permanently deletes every alert those currently match. This cannot be undone.`;
            })()}
          </DialogBody>
          <DialogFooter>
            <Button variant="secondary" onClick={() => setRetroConfirm(null)} disabled={retroBusy}>
              Cancel
            </Button>
            <Button
              variant="danger"
              leadingIcon="trash"
              loading={retroBusy}
              disabled={retroBusy}
              onClick={() => {
                const rows = retroConfirm ?? [];
                void (async () => {
                  setRetroBusy(true);
                  try {
                    await doRetroApply(rows);
                  } finally {
                    setRetroBusy(false);
                    setRetroConfirm(null);
                  }
                })();
              }}
            >
              Delete matching alerts
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
