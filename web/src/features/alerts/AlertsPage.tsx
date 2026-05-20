import { useCallback, useMemo, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { DataTable, type RowAction } from "@/shared/ui/DataTable";
import type { ContextMenuItem } from "@/shared/ui/DataTableContextMenu";
import { Switch } from "@/shared/ui/Switch";
import { Tooltip } from "@/shared/ui/Tooltip";
import { toast } from "@/shared/ui/toast/useToast";
import { Button } from "@/shared/ui/Button";
import { ApiError } from "@/lib/api/client";
import {
  ConfirmDeleteDialog,
  useConfirmDelete,
} from "@/shared/ui/resourceContextMenu";
import * as YAML from "yaml";
import { encodeConditionQ } from "@/lib/condition/serialize";
import type { Condition } from "@/lib/condition/types";
import type { ParsedCondition } from "@/shared/ui/SearchBar";
import { Records, useCommentRecord, useShelveRecord } from "./api";
import { AlertRowDetail } from "./AlertRowDetail";
import { AlertsFilters, type AlertFilters } from "./Filters";
import { alertColumns } from "./columns";
import { useAutoRefresh } from "./useAutoRefresh";
import type { AlertState, Record_ } from "./types";
import { tabById, type TabId } from "./tabs";
import { ActionDialog, type ActionType } from "./ActionDialog";
import styles from "./AlertsPage.module.css";

type AlertsSearch = AlertFilters & {
  page?: number;
  orderby?: string;
  asc?: boolean;
};

const PAGE_SIZE = 50;

/**
 * buildQueryParam combines the active lifecycle-tab preset with the
 * SearchBar's DSL condition into a single Condition AST, then encodes it
 * as base64url JSON for the `?q=` query parameter the CRUD layer expects.
 * Returns undefined when both inputs are empty so the URL stays clean and
 * react-query can cache the unfiltered list.
 *
 * The shape we produce is the frontend's `Condition` (type:"AND" / "EQUALS"),
 * which the Go backend's UnmarshalJSON normalises into the canonical
 * `op` form before the database driver translates it.
 */
function buildQueryParam(tab: TabId, dsl: ParsedCondition | null): string | undefined {
  const parts: Condition[] = [];
  const tabCondition = tabById(tab).condition;
  if (tabCondition) parts.push(tabCondition);
  if (dsl && dsl.op !== "" && dsl.op !== "ALWAYS_TRUE") {
    parts.push(dsl as unknown as Condition);
  }
  if (parts.length === 0) return undefined;
  const combined: Condition =
    parts.length === 1 ? (parts[0] as Condition) : { type: "AND", args: parts };
  return encodeConditionQ(combined);
}

async function copyToClipboard(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    /* fall through */
  }
  return false;
}

export function AlertsPage() {
  // useSearch with strict:false returns the validated search params; cast to local type for stronger state/severity literals.
  const search: AlertsSearch = useSearch({ strict: false }) as unknown as AlertsSearch;
  const navigate = useNavigate();
  const auto = useAutoRefresh(5000);
  const commentMut = useCommentRecord();

  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(new Set());
  const [dialog, setDialog] = useState<{ type: ActionType; records: Record_[] } | null>(null);
  const shelveMut = useShelveRecord();
  const removeMut = Records.useRemove();

  const page = search.page ?? 1;
  const orderby = search.orderby ?? "date_epoch";
  const asc = search.asc ?? false;

  const [searchCondition, setSearchCondition] = useState<ParsedCondition | null>(null);
  const activeTab: TabId = search.tab ?? "alerts";

  const filters: AlertFilters = {
    tab: activeTab,
    ...(search.search !== undefined ? { search: search.search } : {}),
    searchCondition,
  };

  // Combine the active tab's preset condition with the SearchBar's DSL
  // condition into a single AND clause sent server-side as ?q=. The "All"
  // tab has a null preset, so a clean DSL query collapses to no filter at
  // all — the request stays cacheable.
  const q = useMemo(() => buildQueryParam(activeTab, searchCondition), [
    activeTab,
    searchCondition,
  ]);

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

  const list = Records.useList(
    {
      offset: (page - 1) * PAGE_SIZE,
      limit: PAGE_SIZE,
      orderby,
      asc,
      ...(q ? { q } : {}),
    },
    {
      ...(auto.intervalMs !== undefined ? { refetchInterval: auto.intervalMs } : {}),
    },
  );

  const filtered = list.data?.data ?? [];

  const confirmDelete = useConfirmDelete<Record_>({
    onDelete: (uid) => removeMut.mutateAsync(uid),
    noun: "alert",
    onAfter: () => setSelectedKeys(new Set()),
  });

  const openDialog = useCallback(
    (type: ActionType, records: Record_[]) => setDialog({ type, records }),
    [],
  );

  const rowActions = useCallback(
    (row: Record_): RowAction[] => {
      const state = (row.state ?? "") as AlertState;
      const isOpen = state === "" || state === "open";
      const isAcked = state === "ack";
      const isClosed = state === "close";
      const isShelved = state === "shelved" || (row.ttl !== undefined && row.ttl < 0);

      const out: RowAction[] = [];

      if (isOpen) {
        out.push({
          key: "ack",
          label: "Acknowledge",
          icon: "thumbs-up",
          onSelect: () => openDialog("ack", [row]),
        });
        out.push({
          key: "close",
          label: "Close",
          icon: "lock",
          onSelect: () => openDialog("close", [row]),
        });
        out.push({
          key: "esc",
          label: "Re-escalate",
          icon: "rotate-cw",
          onSelect: () => openDialog("esc", [row]),
        });
      } else if (isAcked) {
        out.push({
          key: "close",
          label: "Close",
          icon: "lock",
          onSelect: () => openDialog("close", [row]),
        });
        out.push({
          key: "esc",
          label: "Re-escalate",
          icon: "rotate-cw",
          onSelect: () => openDialog("esc", [row]),
        });
      } else if (isClosed) {
        out.push({
          key: "open",
          label: "Re-open",
          icon: "rotate-cw",
          onSelect: () => openDialog("open", [row]),
        });
      }

      out.push({
        key: "comment",
        label: "Comment",
        icon: "message-square",
        onSelect: () => openDialog("comment", [row]),
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
    [openDialog, shelveMut],
  );

  // Right-click context menu. The "Open" item is omitted: DataTable doesn't
  // expose a programmatic-expand API and the chevron in the first column is
  // the canonical way to toggle the inline panel. We keep the universal
  // Copy-as-JSON / Copy-as-YAML pair and append the alert-specific verbs,
  // mirroring the bulk-toolbar surface.
  const contextMenuItems = useCallback(
    (row: Record_): ContextMenuItem[] => {
      const state = (row.state ?? "") as AlertState;
      const isOpen = state === "" || state === "open";
      const isAcked = state === "ack";
      const isClosed = state === "close";

      const items: ContextMenuItem[] = [
        {
          key: "copy-json",
          label: "Copy as JSON",
          icon: "copy",
          onSelect: async () => {
            const ok = await copyToClipboard(JSON.stringify(row, null, 2));
            if (ok) toast.success("Copied JSON to clipboard");
            else toast.error("Clipboard unavailable");
          },
        },
        {
          key: "copy-yaml",
          label: "Copy as YAML",
          icon: "copy",
          onSelect: async () => {
            const ok = await copyToClipboard(YAML.stringify(row));
            if (ok) toast.success("Copied YAML to clipboard");
            else toast.error("Clipboard unavailable");
          },
        },
      ];

      if (isOpen || isAcked) {
        if (isOpen) {
          items.push({
            key: "ack",
            label: "Acknowledge",
            icon: "thumbs-up",
            onSelect: () => openDialog("ack", [row]),
          });
        }
        items.push({
          key: "close",
          label: "Close",
          icon: "lock",
          onSelect: () => openDialog("close", [row]),
        });
        items.push({
          key: "esc",
          label: "Re-escalate",
          icon: "rotate-cw",
          onSelect: () => openDialog("esc", [row]),
        });
      } else if (isClosed) {
        items.push({
          key: "open",
          label: "Re-open",
          icon: "rotate-cw",
          onSelect: () => openDialog("open", [row]),
        });
      }

      items.push({
        key: "comment",
        label: "Comment",
        icon: "message-square",
        onSelect: () => openDialog("comment", [row]),
      });

      items.push({
        key: "delete",
        label: "Delete",
        icon: "trash",
        danger: true,
        disabled: !row.uid,
        onSelect: () => confirmDelete.request([row]),
      });

      return items;
    },
    [openDialog, confirmDelete],
  );

  const bulkActions = useCallback(
    (rows: Record_[]) => {
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
    },
    [],
  );

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
          // The searchCondition / searchError fields are local UI state
          // (they re-derive from the SearchBar's server round-trip on
          // every text change), so we mirror them to React state instead
          // of the URL search params.
          if (next.searchCondition !== undefined) {
            setSearchCondition(next.searchCondition);
          }
          // The "alerts" tab is the default landing — omit it from the
          // URL so deep-links stay clean. Same for an empty search:
          // setting the key to undefined tells TanStack Router to drop it
          // from the URL on the next navigation. The Record<string,
          // unknown> shape sidesteps exactOptionalPropertyTypes, which
          // refuses explicit `undefined` on a typed optional property
          // even though the runtime semantics are identical.
          updateSearch({
            page: 1,
            tab: next.tab && next.tab !== "alerts" ? next.tab : undefined,
            search: next.search || undefined,
          } as Partial<AlertsSearch>);
        }}
      />
      <DataTable
        data={filtered}
        columns={alertColumns}
        rowKey={(r) => r.uid ?? `${r.host ?? ""}-${r.date_epoch ?? 0}`}
        loading={list.isPending}
        selectable
        selectedKeys={selectedKeys}
        onSelectionChange={setSelectedKeys}
        bulkActions={bulkActions}
        toolbarHeader={`${list.data?.meta.total ?? 0} alerts`}
        toolbar={
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
        }
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
        contextMenuItems={contextMenuItems}
        renderExpanded={(row) => <AlertRowDetail row={row} />}
      />
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
      <ConfirmDeleteDialog
        state={confirmDelete.state}
        onCancel={confirmDelete.cancel}
        onConfirm={() => void confirmDelete.confirm()}
      />
    </div>
  );
}
