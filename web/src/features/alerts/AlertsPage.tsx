import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { DataTable, type RowAction } from "@/shared/ui/DataTable";
import type { ContextMenuItem } from "@/shared/ui/DataTableContextMenu";
import { EmptyState } from "@/shared/ui/EmptyState";
import { Switch } from "@/shared/ui/Switch";
import { Tooltip } from "@/shared/ui/Tooltip";
import { toast } from "@/shared/ui/toast/useToast";
import { Button } from "@/shared/ui/Button";
import { ApiError } from "@/lib/api/client";
import { ConfirmDeleteDialog, useConfirmDelete } from "@/shared/ui/resourceContextMenu";
import { encodeConditionQ } from "@/lib/condition/serialize";
import type { Condition } from "@/lib/condition/types";
import type { ParsedCondition } from "@/shared/ui/SearchBar";
import { severityToken } from "@/lib/format/severity-color";
import { Environments } from "@/features/admin/environments/api";
import { Records, useCommentRecord, useShelveRecord } from "./api";
import { AlertRowDetail } from "./AlertRowDetail";
import { ActiveFilters } from "./ActiveFilters";
import { AlertsFilters, type AlertFilters } from "./Filters";
import { alertColumns } from "./columns";
import { useAutoRefresh } from "./useAutoRefresh";
import type { AlertState, Record_ } from "./types";
import { tabById, type TabId } from "./tabs";
import { ActionDialog, type ActionType } from "./ActionDialog";
import { InjectAlertsDialog } from "./InjectAlertsDialog";
import styles from "./AlertsPage.module.css";

/** Short human label for a record used in undo-toast copy ("Acknowledged X"). */
function recordLabel(r: Record_): string {
  return r.host ?? r.message ?? r.uid ?? "alert";
}

type AlertsSearch = AlertFilters & {
  page?: number;
  orderby?: string;
  asc?: boolean;
  /** Comma-separated env UIDs in the URL (parsed/stringified in onChange). */
  env?: string;
  /**
   * Initial SearchBar DSL text. Read once on mount to seed local state,
   * never written back from typing (see the comment block below). Used by
   * deep-links such as the host hyperlink in Teams alert cards:
   * `/web/alerts?search=hash%20%3D%20<hash>`.
   */
  search?: string;
};

// Note: the SearchBar's text used to live here as a URL param, but
// TanStack Router's navigate() is async — that one-render lag let React
// snap the controlled input back to the stale prop value mid-typing, so
// fast keystrokes were getting dropped. The other list pages (rules,
// users, kv …) keep search text in local React state via useTableSearch
// for the same reason. Tab + env stay URL-persisted because they don't
// change on every keystroke.

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
function buildQueryParam(
  tab: TabId,
  dsl: ParsedCondition | null,
  envCondition: Condition | null,
): string | undefined {
  const parts: Condition[] = [];
  const tabCondition = tabById(tab).condition;
  if (tabCondition) parts.push(tabCondition);
  if (dsl && dsl.op !== "" && dsl.op !== "ALWAYS_TRUE") {
    parts.push(dsl as unknown as Condition);
  }
  if (envCondition) parts.push(envCondition);
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
  // Counts how many inline alert details panels are currently open. When
  // non-zero we pause the auto-refresh poll — refetching swaps the row's
  // backing object, which can yank the timeline / JSON viewer the user is
  // reading right out from under them.
  const [expandedCount, setExpandedCount] = useState(0);
  const [dialog, setDialog] = useState<{ type: ActionType; records: Record_[] } | null>(null);
  const [injectOpen, setInjectOpen] = useState(false);
  const shelveMut = useShelveRecord();
  const removeMut = Records.useRemove();

  const page = search.page ?? 1;
  const orderby = search.orderby ?? "date_epoch";
  const asc = search.asc ?? false;

  // Initial SearchBar text is read from `?search=` so deep-links (e.g. the
  // host hyperlink in a Teams alert card pointing at
  // `/web/alerts?search=hash%20%3D%20<hash>`) land with the right filter
  // already applied. We only seed *from* URL → state; we never write the
  // typed text back to the URL because navigate() is async and the round
  // trip drops characters on fast typing (see the comment block above
  // `AlertsSearch`).
  const [searchText, setSearchText] = useState<string>(() => search.search ?? "");
  // lastSeededRef captures the URL value last folded into local state so a
  // *subsequent* external URL change (browser back/forward, another deep
  // link clicked while this page is mounted) re-seeds, while pure typing
  // (which leaves the URL untouched) does not.
  const lastSeededRef = useRef<string | undefined>(search.search);
  useEffect(() => {
    if (search.search !== lastSeededRef.current) {
      lastSeededRef.current = search.search;
      setSearchText(search.search ?? "");
    }
  }, [search.search]);
  const [searchCondition, setSearchCondition] = useState<ParsedCondition | null>(null);
  const activeTab: TabId = search.tab ?? "alerts";

  const selectedEnvs = useMemo<string[]>(
    () => (search.env ? search.env.split(",").filter(Boolean) : []),
    [search.env],
  );

  // Fetch the environment definitions so we can resolve selected UIDs to
  // their stored filter conditions. The bar fetches the same list; the
  // shared queryKey in defineResource dedupes the request.
  const envList = Environments.useList({
    limit: 200,
    orderby: "tree_order",
    asc: true,
  });

  // OR the selected environments' conditions together. A selected env
  // with an empty/ALWAYS_TRUE condition contributes nothing (since OR'ing
  // it would short-circuit to ALWAYS_TRUE and filter nothing out).
  const envCondition = useMemo<Condition | null>(() => {
    if (selectedEnvs.length === 0) return null;
    const byUid = new Map((envList.data?.data ?? []).map((e) => [e.uid ?? "", e]));
    const conds: Condition[] = [];
    for (const uid of selectedEnvs) {
      const env = byUid.get(uid);
      if (!env) continue;
      if (!env.condition || env.condition.type === "ALWAYS_TRUE") continue;
      conds.push(env.condition);
    }
    if (conds.length === 0) return null;
    return conds.length === 1 ? (conds[0] as Condition) : { type: "OR", args: conds };
  }, [selectedEnvs, envList.data]);

  const filters: AlertFilters = {
    tab: activeTab,
    envs: selectedEnvs,
  };

  // Combine the active tab's preset condition with the SearchBar's DSL
  // condition and the OR'd environment filter into a single AND clause
  // sent server-side as ?q=. The "All" tab has a null preset, so a clean
  // DSL query with no env selection collapses to no filter at all — the
  // request stays cacheable.
  const q = useMemo(
    () => buildQueryParam(activeTab, searchCondition, envCondition),
    [activeTab, searchCondition, envCondition],
  );

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

  // Pause auto-refresh while the user is reading at least one inline
  // detail panel — same intent as the existing toggle, but driven by the
  // user's gaze instead of an explicit click.
  const refreshPaused = expandedCount > 0;
  const effectiveIntervalMs =
    auto.intervalMs !== undefined && !refreshPaused ? auto.intervalMs : undefined;

  const list = Records.useList(
    {
      offset: (page - 1) * PAGE_SIZE,
      limit: PAGE_SIZE,
      orderby,
      asc,
      ...(q ? { q } : {}),
    },
    {
      ...(effectiveIntervalMs !== undefined ? { refetchInterval: effectiveIntervalMs } : {}),
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
                await shelveMut.mutateAsync({
                  uid: row.uid ?? "",
                  shelve: !isShelved,
                  currentTTL: row.ttl,
                });
                // Upgrade the old success toast to an undo: the inverse is a
                // single shelve call flipping `shelve` back, restoring the
                // record's prior TTL magnitude.
                toast.undo(`${isShelved ? "Unshelved" : "Shelved"} • ${recordLabel(row)}`, () => {
                  void (async () => {
                    try {
                      await shelveMut.mutateAsync({
                        uid: row.uid ?? "",
                        shelve: isShelved,
                        currentTTL: row.ttl,
                      });
                    } catch (e) {
                      const detail = e instanceof ApiError ? e.detail : "Undo failed";
                      toast.error(detail);
                    }
                  })();
                });
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

  // inlineAction fires an ack/close comment mutation *directly*, skipping the
  // confirm dialog the kebab/bulk paths use. On success it raises an undo
  // toast whose inverse re-opens the record (type:"open").
  //
  // The undo is a COMPENSATING event, not a delete: ack then undo leaves two
  // entries on the record's timeline (the ack and the re-open). We never
  // silently rewrite history — the operator can still see they acked it and
  // then reverted. This matches how the backend's /comment endpoint models
  // state: every transition is an append-only event.
  const inlineAction = useCallback(
    (row: Record_, type: "ack" | "close") => {
      const uid = row.uid ?? "";
      if (!uid) return;
      void (async () => {
        try {
          await commentMut.mutateAsync({ record_uid: uid, type });
          const verb = type === "ack" ? "Acknowledged" : "Closed";
          toast.undo(`${verb} ${recordLabel(row)}`, () => {
            void (async () => {
              try {
                // Compensating re-open — keeps both events on the timeline.
                await commentMut.mutateAsync({ record_uid: uid, type: "open" });
              } catch (e) {
                const detail = e instanceof ApiError ? e.detail : "Undo failed";
                toast.error(detail);
              }
            })();
          });
        } catch (e) {
          const detail = e instanceof ApiError ? e.detail : "Action failed";
          toast.error(detail);
        }
      })();
    },
    [commentMut],
  );

  // quickActions — the hover/focus-revealed inline IconButtons rendered before
  // the kebab. Derived from the same lifecycle state machine as rowActions,
  // capped at ack / close / comment (the three highest-frequency verbs).
  // ack/close run inline via inlineAction (no dialog); comment still opens the
  // dialog because it requires a message.
  const quickActions = useCallback(
    (row: Record_): RowAction[] => {
      const state = (row.state ?? "") as AlertState;
      const isOpen = state === "" || state === "open";
      const isAcked = state === "ack";
      const isClosed = state === "close";

      const out: RowAction[] = [];
      if (isOpen) {
        out.push({
          key: "ack",
          label: "Acknowledge",
          icon: "thumbs-up",
          onSelect: () => inlineAction(row, "ack"),
        });
      }
      if (isOpen || isAcked) {
        out.push({
          key: "close",
          label: "Close",
          icon: "lock",
          onSelect: () => inlineAction(row, "close"),
        });
      }
      if (!isClosed) {
        out.push({
          key: "comment",
          label: "Comment",
          icon: "message-square",
          onSelect: () => openDialog("comment", [row]),
        });
      }
      return out;
    },
    [inlineAction, openDialog],
  );

  // rowKeyBindings — per-row keyboard shortcuts surfaced through DataTable:
  //   a → inline ack (only when the state machine allows it; open rows)
  //   c → open the comment dialog for the focused row
  // `e` (expand) is handled by DataTable itself. Bindings only fire when a
  // row is focused and the user isn't typing into a field.
  const rowKeyBindings = useCallback(
    (row: Record_): Record<string, () => void> => {
      const state = (row.state ?? "") as AlertState;
      const isOpen = state === "" || state === "open";
      const bindings: Record<string, () => void> = {
        c: () => openDialog("comment", [row]),
      };
      if (isOpen) bindings.a = () => inlineAction(row, "ack");
      return bindings;
    },
    [inlineAction, openDialog],
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
            // Lazily pull in the yaml library — it's only needed for this
            // one rarely-used action, so it stays out of the main bundle.
            const { stringify } = await import("yaml");
            const ok = await copyToClipboard(stringify(row));
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
        // Bulk ack/close keep the confirm dialog but gain the same undo
        // affordance as the inline path: a single re-open of every uid that
        // succeeded (compensating events — the ack/close stays on each
        // record's timeline). esc/comment have no meaningful single-step
        // inverse, so they keep the plain success toast.
        const undoableUids =
          type === "ack" || type === "close" ? records.map((r) => r.uid ?? "").filter(Boolean) : [];
        if (undoableUids.length > 0) {
          // Keep the documented count-bearing copy ("N alerts updated") AND the
          // undo affordance: the description states how many records changed,
          // the Undo button re-opens every uid that succeeded (compensating
          // events). esc/comment fall through to the plain success toast below.
          toast.undo(`${ok} alert${ok === 1 ? "" : "s"} updated`, () => {
            void (async () => {
              const undoResults = await Promise.allSettled(
                undoableUids.map((uid) =>
                  commentMut.mutateAsync({ record_uid: uid, type: "open" }),
                ),
              );
              const undoFailed = undoResults.filter((r) => r.status === "rejected").length;
              if (undoFailed > 0) toast.error(`Undo failed for ${undoFailed} alerts`);
            })();
          });
        } else {
          toast.success(`${ok} alert${ok === 1 ? "" : "s"} updated`);
        }
        setDialog(null);
        setSelectedKeys(new Set());
      } else {
        toast.error(`${failed} of ${records.length} failed; ${ok} succeeded`);
      }
    },
    [commentMut, dialog],
  );

  // Distinguish a genuinely empty install (no alerts ingested yet) from a
  // filter/search/tab that simply matches nothing. Only the former offers the
  // "how to inject alerts" guidance; the latter nudges the operator to widen
  // their filter. The default "alerts" tab preset does not count as a filter.
  const hasActiveFilters =
    searchText.trim() !== "" ||
    searchCondition !== null ||
    selectedEnvs.length > 0 ||
    activeTab !== "alerts";

  // Resolve an env UID to its display name for the ActiveFilters chips. Falls
  // back to the UID when the env list hasn't loaded or the env was deleted.
  const envName = useCallback(
    (uid: string) => {
      const env = (envList.data?.data ?? []).find((e) => e.uid === uid);
      return env?.name ?? uid;
    },
    [envList.data],
  );

  // ActiveFilters chip removers. Tab + env live in the URL (one updateSearch
  // each); the DSL search lives in local state (clear both the text and the
  // parsed condition). "Clear all" resets every source in a single navigation.
  const removeEnv = useCallback(
    (uid: string) => {
      const next = selectedEnvs.filter((u) => u !== uid);
      // Cast through unknown: exactOptionalPropertyTypes refuses an explicit
      // `undefined` on a typed optional prop even though the runtime
      // drop-the-key semantics are exactly what we want (TanStack Router
      // omits undefined keys from the URL). Same trick the onChange handler
      // below uses for its env reset.
      updateSearch({
        page: 1,
        env: next.length > 0 ? next.join(",") : undefined,
      } as unknown as Partial<AlertsSearch>);
    },
    [selectedEnvs, updateSearch],
  );
  const clearTab = useCallback(() => {
    updateSearch({ page: 1, tab: undefined } as unknown as Partial<AlertsSearch>);
  }, [updateSearch]);
  const clearSearchText = useCallback(() => {
    setSearchText("");
    setSearchCondition(null);
    if (page !== 1) updateSearch({ page: 1 });
  }, [page, updateSearch]);
  const clearAllFilters = useCallback(() => {
    setSearchText("");
    setSearchCondition(null);
    updateSearch({ page: 1, tab: undefined, env: undefined } as unknown as Partial<AlertsSearch>);
  }, [updateSearch]);

  // Stable function props for DataTable. Each is memoized so the row-level
  // memo in DataTable holds across AlertsPage re-renders (poll refetches,
  // selection/expansion changes) — otherwise a fresh closure every render
  // would defeat the shallow row comparison and re-render all 50 rows.
  const rowKey = useCallback((r: Record_) => r.uid ?? `${r.host ?? ""}-${r.date_epoch ?? 0}`, []);
  const rowAccent = useCallback((r: Record_) => severityToken(r.severity ?? ""), []);
  const renderExpanded = useCallback((row: Record_) => <AlertRowDetail row={row} />, []);
  const handleExpandedChange = useCallback(
    (keys: ReadonlySet<string>) => setExpandedCount(keys.size),
    [],
  );
  const handleSearchChange = useCallback(
    (c: { text: string; condition: ParsedCondition | null }) => {
      setSearchText(c.text);
      setSearchCondition(c.condition);
      if (page !== 1) updateSearch({ page: 1 });
    },
    [page, updateSearch],
  );
  const handleSortChange = useCallback(
    (next: { sortBy: string; order: "asc" | "desc" }) =>
      updateSearch({ orderby: next.sortBy, asc: next.order === "asc", page: 1 }),
    [updateSearch],
  );
  const handlePageChange = useCallback(
    (next: { page: number }) => updateSearch({ page: next.page }),
    [updateSearch],
  );
  const searchProp = useMemo(
    () => ({ value: searchText, onChange: handleSearchChange, collection: "record" }),
    [searchText, handleSearchChange],
  );
  const sortOrder: "asc" | "desc" = asc ? "asc" : "desc";
  const serverSort = useMemo(
    () => ({
      sortBy: orderby,
      order: sortOrder,
      onChange: handleSortChange,
    }),
    [orderby, sortOrder, handleSortChange],
  );

  const emptyState = hasActiveFilters ? (
    <EmptyState
      icon="search"
      title="No alerts match your filters"
      description="Try widening your search, clearing the environment filter, or switching tabs."
    />
  ) : (
    <EmptyState
      icon="bell-off"
      title="No alerts yet"
      description="Snooze hasn't received any alerts. Connect a monitoring source to start ingesting."
      action={
        <Button variant="primary" leadingIcon="book" onClick={() => setInjectOpen(true)}>
          How to inject alerts
        </Button>
      }
    />
  );

  return (
    <div className={styles.page}>
      <AlertsFilters
        value={filters}
        onChange={(next) => {
          // The "alerts" tab is the default landing — omit it from the
          // URL so deep-links stay clean. Same for an empty env list:
          // setting the key to undefined tells TanStack Router to drop it
          // from the URL on the next navigation. The Record<string,
          // unknown> shape sidesteps exactOptionalPropertyTypes, which
          // refuses explicit `undefined` on a typed optional property
          // even though the runtime semantics are identical.
          const nextEnv = next.envs && next.envs.length > 0 ? next.envs.join(",") : undefined;
          updateSearch({
            page: 1,
            tab: next.tab && next.tab !== "alerts" ? next.tab : undefined,
            env: nextEnv,
          } as Partial<AlertsSearch>);
        }}
      />
      {hasActiveFilters ? (
        <ActiveFilters
          tab={activeTab}
          envs={selectedEnvs}
          envName={envName}
          search={searchText}
          onRemoveEnv={removeEnv}
          onClearTab={clearTab}
          onClearSearch={clearSearchText}
          onClearAll={clearAllFilters}
        />
      ) : null}
      <div id="alerts-panel" role="tabpanel" aria-labelledby={`alerts-tab-${activeTab}`}>
        <DataTable
          data={filtered}
          columns={alertColumns}
          rowKey={rowKey}
          loading={list.isPending}
          stale={list.isPlaceholderData}
          emptyState={emptyState}
          selectable
          selectedKeys={selectedKeys}
          onSelectionChange={setSelectedKeys}
          bulkActions={bulkActions}
          // SearchBar lives in DataTable's toolbar row so the bulk-action
          // bar that appears on row selection sits next to it instead of
          // dropping below. Matches every other list page.
          //
          // The text + parsed condition are both local React state. The
          // SearchBar owns the draft and notifies at parse-resolution cadence,
          // so the parent re-renders when a parse lands — not per keystroke.
          // Pagination still resets to page 1 on every change.
          search={searchProp}
          toolbarHeader={`${list.data?.meta.total ?? 0} alerts`}
          toolbar={
            <Tooltip
              content={
                !auto.enabled
                  ? "Auto-refresh off"
                  : refreshPaused
                    ? "Auto-refresh paused while a row is expanded"
                    : "Auto-refresh every 5s"
              }
            >
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
          serverSort={serverSort}
          serverPagination={{
            page,
            pageSize: PAGE_SIZE,
            total: list.data?.meta.total ?? 0,
            onChange: handlePageChange,
          }}
          rowActions={rowActions}
          quickActions={quickActions}
          rowKeyBindings={rowKeyBindings}
          rowAccent={rowAccent}
          contextMenuItems={contextMenuItems}
          renderExpanded={renderExpanded}
          // Uncontrolled expansion (Phase 2's default path): DataTable owns the
          // expanded set and reports size changes here so we can pause polling
          // while the operator reads an inline panel. Keeping the uncontrolled
          // path means the `e`-to-expand shortcut and chevron both keep working
          // without AlertsPage tracking the key set.
          onExpandedChange={handleExpandedChange}
        />
      </div>
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
      <InjectAlertsDialog open={injectOpen} onOpenChange={setInjectOpen} />
    </div>
  );
}
