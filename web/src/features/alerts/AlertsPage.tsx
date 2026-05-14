import { useCallback, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { DataTable, type RowAction } from "@/shared/ui/DataTable";
import { Switch } from "@/shared/ui/Switch";
import { Tooltip } from "@/shared/ui/Tooltip";
import { toast } from "@/shared/ui/toast/useToast";
import { Button } from "@/shared/ui/Button";
import { ApiError } from "@/lib/api/client";
import { Records, useCommentRecord, useShelveRecord } from "./api";
import { AlertDetailDrawer } from "./AlertDetailDrawer";
import { AlertsFilters, type AlertFilters } from "./Filters";
import { alertColumns } from "./columns";
import { useAutoRefresh } from "./useAutoRefresh";
import type { AlertState, Record_ } from "./types";
import { ActionDialog, type ActionType } from "./ActionDialog";
import styles from "./AlertsPage.module.css";

type AlertsSearch = AlertFilters & {
  page?: number;
  orderby?: string;
  asc?: boolean;
  uid?: string;
};

const PAGE_SIZE = 50;

export function AlertsPage() {
  // useSearch with strict:false returns the validated search params; cast to local type for stronger state/severity literals.
  const search: AlertsSearch = useSearch({ strict: false }) as unknown as AlertsSearch;
  const navigate = useNavigate();
  const auto = useAutoRefresh(5000);
  const commentMut = useCommentRecord();

  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(new Set());
  const [dialog, setDialog] = useState<{ type: ActionType; records: Record_[] } | null>(null);
  const shelveMut = useShelveRecord();

  const page = search.page ?? 1;
  const orderby = search.orderby ?? "date_epoch";
  const asc = search.asc ?? false;
  const detailUid = search.uid;

  const filters: AlertFilters = {
    ...(search.state !== undefined ? { state: search.state } : {}),
    ...(search.severity !== undefined ? { severity: search.severity } : {}),
    ...(search.environment !== undefined ? { environment: search.environment } : {}),
    ...(search.search !== undefined ? { search: search.search } : {}),
  };

  const updateSearch = useCallback(
    (next: Partial<AlertsSearch>) => {
      // TanStack Router's navigate types are locked to the registered route tree at
      // build time. Casting through unknown avoids the "unsafe call" lint issue while
      // still satisfying the type checker when the route is fully registered.
      type NavigateFn = (opts: {
        to: string;
        search: (prev: AlertsSearch | undefined) => AlertsSearch;
      }) => Promise<void>;
      void (navigate as unknown as NavigateFn)({
        to: "/web/alerts",
        search: (prev: AlertsSearch | undefined) => ({ ...(prev ?? {}), ...next }),
      });
    },
    [navigate],
  );

  const closeDrawer = useCallback(() => {
    type NavigateFn = (opts: {
      to: string;
      search: (prev: AlertsSearch | undefined) => AlertsSearch;
    }) => Promise<void>;
    void (navigate as unknown as NavigateFn)({
      to: "/web/alerts",
      search: (prev: AlertsSearch | undefined) => {
        const { uid: _uid, ...rest } = prev ?? {}; // eslint-disable-line @typescript-eslint/no-unused-vars
        return rest as AlertsSearch;
      },
    });
  }, [navigate]);

  const list = Records.useList(
    {
      offset: (page - 1) * PAGE_SIZE,
      limit: PAGE_SIZE,
      orderby,
      asc,
      ...(filters.search ? { search: filters.search } : {}),
    },
    {
      ...(auto.intervalMs !== undefined ? { refetchInterval: auto.intervalMs } : {}),
    },
  );

  // Conditions land in M4; M3.1 post-filters state/severity/environment client-side.
  const filtered = (list.data?.data ?? []).filter((r) => {
    if (filters.state !== undefined && (r.state ?? "") !== filters.state) return false;
    if (filters.severity !== undefined && r.severity !== filters.severity) return false;
    if (filters.environment !== undefined && r.environment !== filters.environment) return false;
    return true;
  });

  const rowActions = useCallback(
    (row: Record_): RowAction[] => {
      const state = (row.state ?? "") as AlertState;
      const isOpen = state === "" || state === "open";
      const isAcked = state === "ack";
      const isClosed = state === "close";
      const isShelved = state === "shelved" || (row.ttl !== undefined && row.ttl < 0);

      const out: RowAction[] = [];

      const openDialog = (type: ActionType) => setDialog({ type, records: [row] });

      if (isOpen) {
        out.push({
          key: "ack",
          label: "Acknowledge",
          icon: "thumbs-up",
          onSelect: () => openDialog("ack"),
        });
        out.push({
          key: "close",
          label: "Close",
          icon: "lock",
          onSelect: () => openDialog("close"),
        });
        out.push({
          key: "esc",
          label: "Re-escalate",
          icon: "rotate-cw",
          onSelect: () => openDialog("esc"),
        });
      } else if (isAcked) {
        out.push({
          key: "close",
          label: "Close",
          icon: "lock",
          onSelect: () => openDialog("close"),
        });
        out.push({
          key: "esc",
          label: "Re-escalate",
          icon: "rotate-cw",
          onSelect: () => openDialog("esc"),
        });
      } else if (isClosed) {
        out.push({
          key: "open",
          label: "Re-open",
          icon: "rotate-cw",
          onSelect: () => openDialog("open"),
        });
      }

      out.push({
        key: "comment",
        label: "Comment",
        icon: "message-square",
        onSelect: () => openDialog("comment"),
      });

      if (!isClosed) {
        out.push({
          key: isShelved ? "unshelve" : "shelve",
          label: isShelved ? "Unshelve" : "Shelve",
          icon: isShelved ? "eye" : "eye-off",
          onSelect: () => {
            void (async () => {
              try {
                await shelveMut.mutateAsync({ uid: row.uid ?? "", shelve: !isShelved });
                toast.success(
                  `${isShelved ? "Unshelved" : "Shelved"} • ${row.host ?? row.uid ?? ""}`,
                );
              } catch (e) {
                const detail = e instanceof ApiError ? e.detail : "Action failed";
                toast.error(detail);
              }
            })();
          },
        });
      }

      return out;
    },
    [shelveMut],
  );

  const bulkActions = useCallback((rows: Record_[]) => {
    const openBulkDialog = (type: ActionType) => setDialog({ type, records: rows });
    return (
      <>
        <Button
          size="sm"
          variant="secondary"
          leadingIcon="thumbs-up"
          onClick={() => openBulkDialog("ack")}
        >
          Acknowledge ({rows.length})
        </Button>
        <Button
          size="sm"
          variant="secondary"
          leadingIcon="lock"
          onClick={() => openBulkDialog("close")}
        >
          Close ({rows.length})
        </Button>
        <Button
          size="sm"
          variant="secondary"
          leadingIcon="rotate-cw"
          onClick={() => openBulkDialog("esc")}
        >
          Re-escalate ({rows.length})
        </Button>
        <Button
          size="sm"
          variant="secondary"
          leadingIcon="message-square"
          onClick={() => openBulkDialog("comment")}
        >
          Comment ({rows.length})
        </Button>
      </>
    );
  }, []);

  const submitDialog = useCallback(
    async ({ message }: { message: string }) => {
      if (!dialog) return;
      const { type, records } = dialog;
      const results = await Promise.allSettled(
        records.map((r) =>
          commentMut.mutateAsync({
            record_uid: r.uid ?? "",
            type,
            ...(message ? { message } : {}),
          }),
        ),
      );
      const ok = results.filter((r) => r.status === "fulfilled").length;
      const failed = results.length - ok;
      if (failed === 0) {
        toast.success(`${ok} alert${ok === 1 ? "" : "s"} updated`);
        setDialog(null);
        setSelectedKeys(new Set());
      } else {
        toast.error(`${failed} of ${records.length} failed; ${ok} succeeded`);
      }
    },
    [commentMut, dialog],
  );

  return (
    <div className={styles.page}>
      <AlertsFilters
        value={filters}
        onChange={(next) => {
          const partial: Partial<AlertsSearch> = { page: 1 };
          if (next.state !== undefined) partial.state = next.state;
          if (next.severity !== undefined) partial.severity = next.severity;
          if (next.environment !== undefined) partial.environment = next.environment;
          if (next.search !== undefined) partial.search = next.search;
          updateSearch(partial);
        }}
      />
      <div className={styles.topbar}>
        <span>{list.data?.meta.total ?? 0} alerts</span>
        <Tooltip content={auto.enabled ? "Auto-refresh every 5s" : "Auto-refresh off"}>
          {/* Switch renders as a button; use div+aria-label instead of label to satisfy a11y rules */}
          <div className={styles.refreshToggle} role="group" aria-label="Auto refresh toggle">
            <span aria-hidden="true">Auto refresh</span>
            <Switch
              checked={auto.enabled}
              onCheckedChange={auto.setEnabled}
              aria-label="Auto refresh"
            />
          </div>
        </Tooltip>
      </div>
      <DataTable
        data={filtered}
        columns={alertColumns}
        rowKey={(r) => r.uid ?? `${r.host ?? ""}-${r.date_epoch ?? 0}`}
        loading={list.isPending}
        selectable
        selectedKeys={selectedKeys}
        onSelectionChange={setSelectedKeys}
        bulkActions={bulkActions}
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
        rowActions={rowActions}
        onRowOpen={(row) => {
          const uid = row.uid;
          updateSearch(uid !== undefined ? { uid } : {});
        }}
      />
      <AlertDetailDrawer uid={detailUid} onClose={closeDrawer} />
      {dialog ? (
        <ActionDialog
          open
          onOpenChange={(o) => {
            if (!o) setDialog(null);
          }}
          actionType={dialog.type}
          records={dialog.records}
          onConfirm={submitDialog}
          submitting={commentMut.isPending}
        />
      ) : null}
    </div>
  );
}
