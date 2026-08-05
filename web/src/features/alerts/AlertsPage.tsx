import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { DataTable, type RowAction } from "@/shared/ui/DataTable";
import type { ContextMenuItem } from "@/shared/ui/DataTableContextMenu";
import { EmptyState } from "@/shared/ui/EmptyState";
import { IconButton } from "@/shared/ui/IconButton";
import { Switch } from "@/shared/ui/Switch";
import { Tooltip } from "@/shared/ui/Tooltip";
import { toast } from "@/shared/ui/toast/useToast";
import { Button } from "@/shared/ui/Button";
import { Badge } from "@/shared/ui/Badge";
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogTitle } from "@/shared/ui/Dialog";
import { ApiError } from "@/lib/api/client";
import { copyToClipboard } from "@/lib/clipboard";
import { expandTemplate } from "@/lib/clipboard-template";
import { ConfirmDeleteDialog, useConfirmDelete } from "@/shared/ui/resourceContextMenu";
import { encodeConditionQ } from "@/lib/condition/serialize";
import { parseText } from "@/lib/condition/text";
import type { Condition } from "@/lib/condition/types";
import type { ParsedCondition } from "@/shared/ui/SearchBar";
import { severityToken } from "@/lib/format/severity-color";
import { useConsoleConfig } from "@/features/config/api";
import { Environments } from "@/features/admin/environments/api";
import type { IconName } from "@/shared/icons/icon-names";
import {
  Records,
  useCommentRecord,
  useShelveRecord,
  useBulkStateRecord,
  encodeUidsAsQ,
} from "./api";
import { AlertRowDetail } from "./AlertRowDetail";
import { ActiveFilters } from "./ActiveFilters";
import { AlertsFilters, type AlertFilters } from "./Filters";
import { SavedSearches } from "./SavedSearches";
import { alertColumns, recordCommentCount } from "./columns";
import { useAutoRefresh } from "./useAutoRefresh";
import type { Record_, AlertState } from "./types";
import { tabById, type TabId } from "./tabs";
import { ActionDialog, type ActionType } from "./ActionDialog";
import { ShelveDialog } from "./ShelveDialog";
import { BulkTagDialog } from "./BulkTagDialog";
import { InjectAlertsDialog } from "./InjectAlertsDialog";
import { isActionAllowed, validBulkStates, BULK_STATE_CAVEAT } from "./transitions";
import styles from "./AlertsPage.module.css";

// Module-scope constant so the reference is stable across renders (DataTable's
// row memo depends on stable rowActions identity). comment/shelve are always-
// allowed and appended unconditionally after the filtered set.
const CANDIDATE_ROW_ACTIONS: Array<{ key: ActionType; label: string; icon: IconName }> = [
  { key: "ack", label: "Acknowledge", icon: "thumbs-up" },
  { key: "close", label: "Close", icon: "lock" },
  { key: "esc", label: "Re-escalate", icon: "rotate-cw" },
  { key: "open", label: "Re-open", icon: "rotate-cw" },
];

/** Short human label for a record used in undo-toast copy ("Acknowledged X"). */
function recordLabel(r: Record_): string {
  return r.host ?? r.message ?? r.uid ?? "alert";
}

/** host+message AND condition for the "Snooze this alert" action — the two
 *  fields that usually identify "this specific problem" without also
 *  matching every other alert from the same host. Falls back to whichever
 *  field is present when the other is missing. */
function snoozeConditionFor(r: Record_): Condition {
  const parts: Condition[] = [];
  if (r.host) parts.push({ type: "EQUALS", field: "host", value: r.host });
  if (r.message) parts.push({ type: "EQUALS", field: "message", value: r.message });
  if (parts.length === 0) return { type: "ALWAYS_TRUE" };
  return parts.length === 1 ? parts[0]! : { type: "AND", args: parts };
}

/** Stable key for a record row — the uid, or a host+timestamp fallback for the
 *  rare uid-less row. Must match DataTable's `rowKey` so the `?record=` URL
 *  value resolves back to a visible row. Module-scope so it's usable before the
 *  memoized `rowKey` callback is declared (context menu, row-open handler). */
function recordKey(r: Record_): string {
  return r.uid ?? `${r.host ?? ""}-${r.date_epoch ?? 0}`;
}

// Selecting more than this many rows before bulk-snoozing shows a warning
// with a preview, since the resulting OR condition gets broad and hard to
// read back later — better to have the operator glance at what they're
// about to combine into one rule.
const BULK_SNOOZE_WARN_THRESHOLD = 5;

/** OR of each selected row's host+message condition — "snooze if it matches
 *  ANY of these alerts", vs. the single-row AND used by "Snooze this alert". */
function bulkSnoozeCondition(rows: Record_[]): Condition {
  const parts = rows.map(snoozeConditionFor);
  if (parts.length === 0) return { type: "ALWAYS_TRUE" };
  return parts.length === 1 ? parts[0]! : { type: "OR", args: parts };
}

function bulkSnoozeName(rows: Record_[]): string {
  if (rows.length === 1) return `Snooze — ${rows[0]!.host ?? rows[0]!.message ?? "alert"}`;
  return `Snooze — ${rows.length} alerts`;
}

function bulkSnoozeComment(rows: Record_[]): string {
  const r = rows[0];
  if (rows.length === 1 && r) {
    return `Snoozed from alert ${r.uid ?? recordKey(r)}${r.message ? `: ${r.message}` : ""}`;
  }
  const preview = rows
    .slice(0, 3)
    .map((row) => row.host ?? row.uid ?? "alert")
    .join(", ");
  const more = rows.length > 3 ? ` and ${rows.length - 3} more` : "";
  return `Snoozed from ${rows.length} alerts: ${preview}${more}`;
}

/** "host — message" per row, capped, for the bulk-snooze warning dialog. */
function bulkSnoozePreview(rows: Record_[], max = 5): { lines: string[]; more: number } {
  const lines = rows.slice(0, max).map((r) => `${r.host ?? "?"} — ${r.message ?? "(no message)"}`);
  return { lines, more: Math.max(0, rows.length - max) };
}

type AlertsSearch = AlertFilters & {
  page?: number;
  orderby?: string;
  asc?: boolean;
  /** Comma-separated env UIDs in the URL (parsed/stringified in onChange). */
  env?: string;
  /** Open detail-drawer record key (its uid). Round-trips the modal detail
   *  drawer through the URL so an open alert is shareable / deep-linkable. */
  record?: string;
  /**
   * SearchBar DSL text. Seeds local state on mount and re-seeds on external
   * URL changes (browser nav, deep-links such as the host hyperlink in Teams
   * alert cards: `/web/alerts?search=hash%20%3D%20<hash>`). It is *not* written
   * per-keystroke (see the note below) — only on a discrete commit (Enter /
   * clear), handled by `handleSearchSubmit`.
   */
  search?: string;
};

// Note: the SearchBar's text is NOT written to the URL on every keystroke.
// TanStack Router's navigate() is async — that one-render lag let React snap
// the controlled input back to the stale prop value mid-typing, so fast
// keystrokes were getting dropped. Instead we commit on a discrete action:
// pressing Enter (with no autocomplete pick) or clearing the field, via the
// SearchBar's onSubmit. That's safe because the draft is stable at that point.
// The other list pages (rules, users, kv …) still keep search text in local
// React state via useTableSearch. Tab + env are URL-persisted the same way.

const PAGE_SIZE = 50;

// Advertised in the DataTable's "?" shortcuts legend. Mirrors the per-row
// bindings wired in `rowKeyBindings` (a=ack, c=comment); the table prepends its
// own built-in navigation shortcuts (move / open / view / select). Module
// constant so its identity is stable across renders (row-memo contract).
const ALERT_KEYBOARD_HINTS = [
  { keys: "A", label: "Acknowledge focused alert" },
  { keys: "C", label: "Comment on focused alert" },
];

/**
 * parseSortBy splits a `sort_by` string (e.g. "-date_epoch") into a field name
 * and ascending flag. A leading "-" means descending. An empty/undefined input
 * falls back to `fallbackField` descending — matching the prior hardcode.
 */
function parseSortBy(
  sortBy: string | undefined,
  fallbackField: string,
): { field: string; asc: boolean } {
  const raw = (sortBy ?? "").trim();
  if (raw === "") return { field: fallbackField, asc: false };
  if (raw.startsWith("-")) return { field: raw.slice(1), asc: false };
  return { field: raw, asc: true };
}

/**
 * columnsForConfig reorders/filters the static alertColumns to match the
 * server-configured column id list, preserving the ColumnDef cells. Unknown
 * ids in the config are skipped; an empty/undefined list returns every column
 * in its declared order (the prior hardcode).
 */
function columnsForConfig(ids: string[] | undefined): typeof alertColumns {
  if (!ids || ids.length === 0) return alertColumns;
  const byId = new Map(alertColumns.map((c) => [c.id, c]));
  const out = ids.map((id) => byId.get(id)).filter((c): c is (typeof alertColumns)[number] => !!c);
  // Never render an empty table because of a bad config — fall back to the
  // full set when nothing matched.
  return out.length > 0 ? out : alertColumns;
}

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
/** The SearchBar's text, as the frontend's own `Condition` AST (type/args) —
 *  what "the search" means for the snooze-from-search badge and the "select
 *  every matching row" bulk-snooze shortcut, both of which should snooze on
 *  the typed filter, not the page's incidental tab/env view state.
 *
 * Deliberately does NOT reuse the SearchBar's server-parsed `ParsedCondition`
 * (the `dsl` used by {@link buildQueryParam} for the `?q=` list filter) —
 * that's the backend's own wire shape (`op`/`children`, short symbolic ops
 * like `"="`/`"&gt;"`), fine to forward opaquely as JSON for a list filter, but
 * WRONG once it needs to be a real `Condition` that ConditionEditor renders
 * and re-serializes by switching on `.type`/`.args`/`.arg` — feeding it the
 * wire shape left every switch falling through, i.e. a garbled snooze
 * condition. `parseText` is the same DSL parser ConditionEditor's own Text
 * tab uses, so it produces a `Condition` guaranteed compatible with it. */
function searchOnlyCondition(text: string): Condition | null {
  if (!text.trim()) return null;
  const r = parseText(text);
  return r.ok ? r.value : null;
}

function searchSnoozeName(searchText: string): string {
  return `Snooze — search: ${searchText}`;
}

function searchSnoozeComment(searchText: string): string {
  // No match count here: the badge's count comes from the currently visible
  // (tab/env-filtered) list, not from the search condition alone, so it can
  // overstate or understate what this condition — used on its own, without
  // the tab preset — will actually match (e.g. it also catches already-acked
  // alerts the "Alerts" tab hides).
  return `Snoozed from search "${searchText}"`;
}

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

export function AlertsPage() {
  // useSearch with strict:false returns the validated search params; cast to local type for stronger state/severity literals.
  const search: AlertsSearch = useSearch({ strict: false }) as unknown as AlertsSearch;
  const navigate = useNavigate();
  // Server-driven console defaults (GET /api/v1/config). placeholderData means
  // `config` is always a usable document — the current hardcodes until the
  // fetch resolves, and on failure. We read refresh/sort/columns from it with
  // the local fallbacks below.
  const { data: config } = useConsoleConfig();
  // Auto-refresh interval comes from the server config (seconds → ms); the
  // 5000ms fallback matches the prior hardcode if the field is ever missing.
  const refreshMs = (config?.refresh_interval ?? 5) * 1000;
  const auto = useAutoRefresh(refreshMs);
  const commentMut = useCommentRecord();

  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(new Set());
  const [dialog, setDialog] = useState<{ type: ActionType; records: Record_[] } | null>(null);
  const [shelveDialog, setShelveDialog] = useState<Record_[] | null>(null);
  const [bulkTagOpen, setBulkTagOpen] = useState(false);
  const [injectOpen, setInjectOpen] = useState(false);
  // Drives the ActionDialog confirm button across a whole bulk submit. A single
  // shared mutation's isPending only tracks its latest call, so it could flip
  // back to enabled mid-batch (inviting a double-submit); this local flag stays
  // true until every request in the batch settles.
  const [bulkSubmitting, setBulkSubmitting] = useState(false);
  // When selectAllMode is true, bulk actions target the live page query (all
  // matching records) rather than an IN-uid list for the visible selection.
  const [selectAllMode, setSelectAllMode] = useState(false);
  const shelveMut = useShelveRecord();
  const bulkStateMut = useBulkStateRecord();
  const removeMut = Records.useRemove();

  const page = search.page ?? 1;
  // Default sort comes from the server config's `sort_by` (e.g. "-date_epoch":
  // a "-" prefix means descending). The URL search params still win when
  // present; the config only supplies the default. Falls back to the prior
  // hardcode (date_epoch desc) when the field is empty.
  const { field: defaultSortField, asc: defaultSortAsc } = useMemo(
    () => parseSortBy(config?.sort_by, "date_epoch"),
    [config?.sort_by],
  );
  const orderby = search.orderby ?? defaultSortField;
  const asc = search.asc ?? defaultSortAsc;

  // Initial SearchBar text is read from `?search=` so deep-links (e.g. the
  // host hyperlink in a Teams alert card pointing at
  // `/web/alerts?search=hash%20%3D%20<hash>`) land with the right filter
  // already applied. URL → state seeding happens here and in the effect below;
  // state → URL happens only on a committed query (Enter / clear) via
  // handleSearchSubmit, never on per-keystroke typing (navigate() is async and
  // the round trip drops characters — see the comment block above
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

  // Config-seeding: once (and only once) when the server config resolves a
  // non-empty default_filter and the URL has no ?search= of its own. This is
  // symmetric to the URL-seeding effect above. The ref prevents re-seeding on
  // subsequent renders (config is staleTime=Infinity so it won't change, but
  // guard it explicitly so the logic is correct by construction).
  const configDefaultSeededRef = useRef(false);
  useEffect(() => {
    if (
      !configDefaultSeededRef.current && // only once
      !search.search && // URL wins when present
      config?.default_filter // non-empty server default
    ) {
      configDefaultSeededRef.current = true;
      setSearchText(config.default_filter);
    }
  }, [config?.default_filter, search.search]);

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

  // Open detail record (drives the modal detail drawer, synced to the URL as
  // ?record=). Undefined = no drawer open.
  const record = search.record;

  // Pause auto-refresh while the detail drawer is open — refetching swaps the
  // row's backing object, which can yank the timeline / JSON the operator is
  // reading right out from under them.
  const refreshPaused = record !== undefined;
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

  // Audio cue: play once per poll tick that grows the alert count.
  // Guard order:
  //   1. config.audio non-empty — no-op when the field is "" (CONSOLE_FALLBACK default)
  //   2. auto.enabled — user who turned off auto-refresh should not hear cues
  //   3. !refreshPaused — no cue while a detail panel is open
  //   4. prevTotalRef.current >= 0 — skip until at least one successful data load has
  //      established a baseline. The ref starts at -1; it is only updated when
  //      list.data is non-null (i.e. a real server response, not loading state).
  //      This ensures the very first data arrival never triggers audio regardless
  //      of how many "total = 0" (loading) renders preceded it.
  //   5. total > prevTotalRef.current — count actually grew
  const prevTotalRef = useRef<number>(-1);
  useEffect(() => {
    // Only update the baseline when we have real data, not during loading.
    if (!list.data) return;
    const total = list.data.meta.total;
    if (
      config?.audio &&
      auto.enabled &&
      !refreshPaused &&
      prevTotalRef.current >= 0 &&
      total > prevTotalRef.current
    ) {
      new Audio(config.audio).play().catch(() => {
        // Autoplay policy (NotAllowedError) — silently swallow.
        // A prior user gesture (login click) is usually sufficient,
        // but browsers can still block before the first gesture.
      });
    }
    prevTotalRef.current = total;
  }, [list.data, config?.audio, auto.enabled, refreshPaused]);

  const filtered = list.data?.data ?? [];

  const confirmDelete = useConfirmDelete<Record_>({
    onDelete: (uid) => removeMut.mutateAsync(uid),
    noun: "alert",
    // Keep only failed rows selected so a retry targets exactly them.
    onAfter: (failed) => setSelectedKeys(new Set(failed.map((r) => r.uid ?? "").filter(Boolean))),
  });

  const openDialog = useCallback(
    (type: ActionType, records: Record_[]) => setDialog({ type, records }),
    [],
  );

  // "Snooze this alert(s)" — hands off to the snoozes page with a new-snooze
  // form prefilled from the given rows: a host+message condition (OR'd across
  // rows when there's more than one), a generated name/comment, and a 1h
  // window (adjustable before saving).
  const snoozeRows = useCallback(
    (rows: Record_[]) => {
      if (rows.length === 0) return;
      void navigate({
        to: "/web/snoozes",
        search: {
          prefillCond: encodeConditionQ(bulkSnoozeCondition(rows)),
          prefillName: bulkSnoozeName(rows),
          prefillComment: bulkSnoozeComment(rows),
          prefillSeconds: 3600,
        },
      });
    },
    [navigate],
  );

  // "Snooze from search" — the badge next to the SearchBar's clear button,
  // and the no-warning bulk path below both snooze on the typed filter
  // itself rather than an OR of individual rows.
  const snoozeFromCondition = useCallback(
    (condition: Condition, text: string) => {
      void navigate({
        to: "/web/snoozes",
        search: {
          prefillCond: encodeConditionQ(condition),
          prefillName: searchSnoozeName(text),
          prefillComment: searchSnoozeComment(text),
          prefillSeconds: 3600,
        },
      });
    },
    [navigate],
  );

  // Bulk-snoozing more than BULK_SNOOZE_WARN_THRESHOLD rows ORs that many
  // conditions into one rule — easy to do by accident from a big selection,
  // so confirm with a preview first instead of just firing. Exception: when
  // every alert matching a non-empty search is selected (a single page, so
  // `rows` really is the full matching set), snoozing on the search
  // condition itself is exactly as precise as the individual rows would be
  // and reads back far better later — skip the warning and use it directly.
  const [snoozeBulkConfirm, setSnoozeBulkConfirm] = useState<Record_[] | null>(null);
  const requestSnoozeRows = useCallback(
    (rows: Record_[]) => {
      // searchCondition !== null: the backend has accepted searchText as a
      // valid, non-trivial filter — see the note above searchSnoozeCond.
      const searchCond = searchCondition !== null ? searchOnlyCondition(searchText) : null;
      const total = list.data?.meta.total ?? -1;
      const allMatchingSelected =
        searchCond !== null && !selectAllMode && rows.length > 0 && rows.length === total;
      if (allMatchingSelected) {
        snoozeFromCondition(searchCond, searchText);
        return;
      }
      if (rows.length > BULK_SNOOZE_WARN_THRESHOLD) setSnoozeBulkConfirm(rows);
      else snoozeRows(rows);
    },
    [snoozeRows, snoozeFromCondition, searchCondition, searchText, selectAllMode, list.data?.meta.total],
  );

  const rowActions = useCallback(
    (row: Record_): RowAction[] => {
      const state = (row.state ?? "") as AlertState;
      const isClosed = state === "close";
      // ttl<0 branch: legacy permanent-exempt rows (pre-plan-34b); new-model rows use state=="shelved"
      const isShelved = state === "shelved" || (row.ttl !== undefined && row.ttl < 0);

      const out: RowAction[] = [];

      // Flat filter over candidates using the transition gate — replaces the
      // nested if (isOpen) / else if (isAcked) / else if (isClosed) chains.
      for (const { key, label, icon } of CANDIDATE_ROW_ACTIONS) {
        if (isActionAllowed(state, key)) {
          out.push({
            key,
            label,
            icon,
            onSelect: () => openDialog(key, [row]),
          });
        }
      }

      // comment is always-allowed.
      out.push({
        key: "comment",
        label: "Comment",
        icon: "message-square",
        onSelect: () => openDialog("comment", [row]),
      });

      // snooze is always-allowed — it's a preventive action, not a state
      // transition, so it doesn't need transition-gating.
      out.push({
        key: "snooze",
        label: "Snooze this alert",
        icon: "moon",
        onSelect: () => snoozeRows([row]),
      });

      if (!isClosed) {
        if (isShelved) {
          // Unshelve via new comment-based API
          out.push({
            key: "unshelve",
            label: "Unshelve",
            icon: "eye",
            onSelect: () => {
              void (async () => {
                try {
                  await commentMut.mutateAsync({ record_uid: row.uid ?? "", type: "unshelve" });
                  toast.undo(`Unshelved • ${recordLabel(row)}`, () => {
                    void (async () => {
                      try {
                        await commentMut.mutateAsync({ record_uid: row.uid ?? "", type: "shelve" });
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
        } else {
          // Timed shelve via ShelveDialog
          out.push({
            key: "shelve",
            label: "Shelve",
            icon: "eye-off",
            onSelect: () => setShelveDialog([row]),
          });
          // Legacy permanent-exempt (ttl=-1) — kept as a clearly-labelled secondary action
          out.push({
            key: "permanent-exempt",
            label: "Permanent exempt (legacy)",
            icon: "eye-off",
            onSelect: () => {
              void (async () => {
                try {
                  await shelveMut.mutateAsync({
                    uid: row.uid ?? "",
                    shelve: true,
                    currentTTL: row.ttl,
                  });
                  toast.undo(`Permanent exempt • ${recordLabel(row)}`, () => {
                    void (async () => {
                      try {
                        await shelveMut.mutateAsync({
                          uid: row.uid ?? "",
                          shelve: false,
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
      }

      return out;
    },
    [openDialog, shelveMut, commentMut, snoozeRows],
  );

  // Count pill on the kebab: signals a row carries discussion (the full thread
  // lives in the expandable row detail). Only rendered when comment_count > 0.
  const rowActionsBadge = useCallback((row: Record_) => {
    const n = recordCommentCount(row);
    if (n <= 0) return undefined;
    return { count: n, label: `${n} comment${n === 1 ? "" : "s"}` };
  }, []);

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

      const out: RowAction[] = [];
      // ack/close run inline (no dialog) — gate them on the real transition
      // table so escalated rows get quick ack/close and re-opened rows don't
      // offer transitions the backend rejects.
      if (isActionAllowed(state, "ack")) {
        out.push({
          key: "ack",
          label: "Acknowledge",
          icon: "thumbs-up",
          onSelect: () => inlineAction(row, "ack"),
        });
      }
      if (isActionAllowed(state, "close")) {
        out.push({
          key: "close",
          label: "Close",
          icon: "lock",
          onSelect: () => inlineAction(row, "close"),
        });
      }
      // comment is always-allowed.
      out.push({
        key: "comment",
        label: "Comment",
        icon: "message-square",
        onSelect: () => openDialog("comment", [row]),
      });
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
      const bindings: Record<string, () => void> = {
        c: () => openDialog("comment", [row]),
      };
      // 'a' acks inline only where the backend allows it (fresh/open/escalated).
      if (isActionAllowed(state, "ack")) bindings.a = () => inlineAction(row, "ack");
      return bindings;
    },
    [inlineAction, openDialog],
  );

  // Right-click context menu. DataTable auto-prepends its own "View details"
  // item ahead of these (since `renderDetails` is set below), so this list
  // starts with the universal Copy-as-JSON / Copy-as-YAML pair and appends the
  // alert-specific verbs, mirroring the bulk-toolbar surface.
  const contextMenuItems = useCallback(
    (row: Record_): ContextMenuItem[] => {
      const state = (row.state ?? "") as AlertState;

      const items: ContextMenuItem[] = [
        {
          key: "copy-json",
          // When a clipboard_template is configured, the copy action expands it
          // and the label becomes simply "Copy"; without a template it falls back
          // to pretty-printed JSON and keeps the "Copy as JSON" label.
          label: config?.clipboard_template ? "Copy" : "Copy as JSON",
          icon: "copy",
          onSelect: async () => {
            const text = expandTemplate(
              config?.clipboard_template ?? "",
              row as Record<string, unknown>,
            );
            const ok = await copyToClipboard(text);
            if (ok)
              toast.success(
                config?.clipboard_template ? "Copied to clipboard" : "Copied JSON to clipboard",
              );
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

      // Flat filter over candidates using the transition gate.
      for (const { key, label, icon } of CANDIDATE_ROW_ACTIONS) {
        if (isActionAllowed(state, key)) {
          items.push({
            key,
            label,
            icon,
            onSelect: () => openDialog(key, [row]),
          });
        }
      }

      // comment is always-allowed.
      items.push({
        key: "comment",
        label: "Comment",
        icon: "message-square",
        onSelect: () => openDialog("comment", [row]),
      });

      items.push({
        key: "snooze",
        label: "Snooze this alert",
        icon: "moon",
        onSelect: () => snoozeRows([row]),
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
    [openDialog, confirmDelete, config?.clipboard_template, snoozeRows],
  );

  const bulkActions = useCallback(
    (rows: Record_[]) => {
      const openBulkDialog = (type: ActionType) => setDialog({ type, records: rows });
      const total = list.data?.meta.total ?? 0;
      const pageCount = rows.length;

      // Transition eligibility: intersection of valid moves across all selected
      // rows. When selectAllMode is true we cannot know off-page states, so we
      // show all state buttons as enabled (with a tooltip caveat handled via
      // the disabled prop being false).
      const valid = selectAllMode
        ? new Set<ActionType>(["ack", "close", "open", "esc"])
        : validBulkStates(rows);

      // Label suffix: show "all N" when operating on the full query scope.
      const countLabel = selectAllMode ? `all ${total}` : String(pageCount);

      return (
        <>
          {/* State-transition buttons gated on validity */}
          {valid.has("ack") ? (
            <Button
              size="sm"
              variant="secondary"
              leadingIcon="thumbs-up"
              onClick={() => openBulkDialog("ack")}
            >
              Acknowledge ({countLabel})
            </Button>
          ) : null}
          {valid.has("close") ? (
            <Button
              size="sm"
              variant="secondary"
              leadingIcon="lock"
              onClick={() => openBulkDialog("close")}
            >
              Close ({countLabel})
            </Button>
          ) : null}
          {valid.has("esc") ? (
            <Button
              size="sm"
              variant="secondary"
              leadingIcon="rotate-cw"
              onClick={() => openBulkDialog("esc")}
            >
              Re-escalate ({countLabel})
            </Button>
          ) : null}
          {valid.has("open") ? (
            <Button
              size="sm"
              variant="secondary"
              leadingIcon="rotate-cw"
              onClick={() => openBulkDialog("open")}
            >
              Re-open ({countLabel})
            </Button>
          ) : null}
          {/* comment is always-allowed */}
          <Button
            size="sm"
            variant="secondary"
            leadingIcon="message-square"
            onClick={() => openBulkDialog("comment")}
          >
            Comment ({pageCount})
          </Button>
          {/* Tag / set fields */}
          <Button
            size="sm"
            variant="secondary"
            leadingIcon="edit"
            onClick={() => setBulkTagOpen(true)}
          >
            Tag / set fields ({countLabel})
          </Button>
          {/* Snooze — built from the actual row objects (host+message per
              row), so it's not offered in selectAllMode where off-page rows
              aren't loaded. */}
          {!selectAllMode && pageCount > 0 ? (
            <Button
              size="sm"
              variant="secondary"
              leadingIcon="moon"
              onClick={() => requestSnoozeRows(rows)}
            >
              Snooze ({pageCount})
            </Button>
          ) : null}
          {/* "Select all N matching this filter" affordance */}
          {!selectAllMode && total > pageCount && pageCount > 0 ? (
            <Button
              size="sm"
              variant="ghost"
              onClick={() => {
                setSelectAllMode(true);
              }}
            >
              Select all {total} matching this filter
            </Button>
          ) : null}
          {selectAllMode ? (
            <Button
              size="sm"
              variant="ghost"
              onClick={() => {
                setSelectAllMode(false);
              }}
            >
              Clear selection scope
            </Button>
          ) : null}
        </>
      );
    },
    [list.data?.meta.total, selectAllMode, requestSnoozeRows],
  );

  const submitDialog = useCallback(
    async ({ message }: { message: string }) => {
      if (!dialog) return;
      const { type, records } = dialog;

      if (type === "comment") {
        // comment still uses the per-record /comment loop (bulk_state does not
        // write per-record notes).
        setBulkSubmitting(true);
        let results: PromiseSettledResult<unknown>[];
        try {
          results = await Promise.allSettled(
            records.map((r) =>
              commentMut.mutateAsync({
                record_uid: r.uid ?? "",
                type,
                ...(message ? { message } : {}),
              }),
            ),
          );
        } finally {
          setBulkSubmitting(false);
        }
        const ok = results.filter((r) => r.status === "fulfilled").length;
        const failed = results.length - ok;
        if (failed === 0) {
          toast.success(`${ok} alert${ok === 1 ? "" : "s"} updated`);
          setDialog(null);
          setSelectedKeys(new Set());
        } else {
          // Surface the backend's actual reason (e.g. an invalid-transition 403)
          // instead of a bare count, so the operator knows why it failed and
          // whether to retry.
          const firstRejected = results.find(
            (r): r is PromiseRejectedResult => r.status === "rejected",
          );
          const detail =
            firstRejected && firstRejected.reason instanceof ApiError
              ? firstRejected.reason.detail
              : "";
          toast.error(
            `${failed} of ${records.length} failed${detail ? `: ${detail}` : ""}; ${ok} succeeded`,
          );
        }
        return;
      }

      // State-transition types (ack|close|open|esc): one bulk_state call.
      // selectAllMode → use the live page query (all matching records);
      // default → build an IN-uid condition from the visible selection.
      const bulkQ = selectAllMode ? q : encodeUidsAsQ(records.map((r) => r.uid ?? ""));
      setBulkSubmitting(true);
      try {
        const resp = await bulkStateMut.mutateAsync({
          ...(bulkQ ? { q: bulkQ } : {}),
          state: type,
          ...(message ? { message } : {}),
        });
        const pl = resp.matched === 1 ? "" : "s";
        const mainMsg = `${resp.matched} alert${pl} updated`;
        const partialNote =
          resp.matched !== resp.updated
            ? ` (${resp.updated} changed, ${resp.matched - resp.updated} already in target state)`
            : "";
        // Description must carry both the count and the caveat because tests
        // assert against t.description.
        toast.success(`${mainMsg}${partialNote} — ${BULK_STATE_CAVEAT}`);
        setDialog(null);
        setSelectedKeys(new Set());
        setSelectAllMode(false);
      } catch (e) {
        const detail = e instanceof ApiError ? e.detail : "Bulk action failed";
        toast.error(detail);
      } finally {
        setBulkSubmitting(false);
      }
    },
    [commentMut, bulkStateMut, dialog, selectAllMode, q],
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

  // Whether the ActiveFilters chip strip should render. It only carries tab +
  // env chips now (search shows in the SearchBar itself, with its own clear),
  // so a search-only filter leaves the strip empty — gate on tab/env alone.
  const hasChipFilters = selectedEnvs.length > 0 || activeTab !== "alerts";

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
  const clearAllFilters = useCallback(() => {
    setSearchText("");
    setSearchCondition(null);
    updateSearch({
      page: 1,
      tab: undefined,
      env: undefined,
      search: undefined,
    } as unknown as Partial<AlertsSearch>);
  }, [updateSearch]);

  // Stable function props for DataTable. Each is memoized so the row-level
  // memo in DataTable holds across AlertsPage re-renders (poll refetches,
  // selection/expansion changes) — otherwise a fresh closure every render
  // would defeat the shallow row comparison and re-render all 50 rows.
  // Columns are server-configurable (console.columns). Memoize so the
  // identity is stable across re-renders (DataTable's row memo depends on it).
  const columns = useMemo(() => columnsForConfig(config?.columns), [config?.columns]);
  const rowKey = useCallback((r: Record_) => recordKey(r), []);
  const rowAccent = useCallback((r: Record_) => severityToken(r.severity ?? ""), []);
  const renderDetails = useCallback((row: Record_) => <AlertRowDetail row={row} />, []);
  // Drawer title: the alert's host in mono (falls back to uid). Host is not
  // repeated in the drawer body, so this is where the operator reads it.
  const detailsTitle = useCallback(
    (r: Record_) => <span className={styles.detailsHost}>{r.host ?? r.uid ?? "alert"}</span>,
    [],
  );
  // Controlled detail drawer: write the open record to the URL (?record=),
  // dropping the key when the drawer closes so deep-links stay clean.
  const handleDetailsKeyChange = useCallback(
    (k: string | null) =>
      updateSearch({ record: k ?? undefined } as unknown as Partial<AlertsSearch>),
    [updateSearch],
  );
  const handleSearchChange = useCallback(
    (c: { text: string; condition: ParsedCondition | null }) => {
      setSearchText(c.text);
      setSearchCondition(c.condition);
      if (page !== 1) updateSearch({ page: 1 });
    },
    [page, updateSearch],
  );
  // Commit-on-Enter (and clear): the SearchBar only fires this once the query
  // parses cleanly, so we can safely fold the text into the URL as ?search=.
  // This is the deliberate exception to "search text is never written back" —
  // a discrete action, not per-keystroke, so navigate()'s async lag can't drop
  // characters. An empty commit drops the key, keeping deep-links clean.
  const handleSearchSubmit = useCallback(
    (text: string) => {
      const trimmed = text.trim();
      updateSearch({
        search: trimmed ? text : undefined,
        page: 1,
      } as unknown as Partial<AlertsSearch>);
    },
    [updateSearch],
  );
  const handleSortChange = useCallback(
    (next: { sortBy: string; order: "asc" | "desc" }) =>
      updateSearch({ orderby: next.sortBy, asc: next.order === "asc", page: 1 }),
    [updateSearch],
  );
  const handlePageChange = useCallback(
    (next: { page: number }) => {
      setSelectAllMode(false);
      updateSearch({ page: next.page });
    },
    [updateSearch],
  );
  // Badge next to the SearchBar's clear button — appears once the typed
  // filter (ignoring tab/env) matches at least one alert, and jumps straight
  // to a new snooze prefilled with that same filter. Deliberately doesn't
  // show a count: `searchMatchCount` is the currently *visible* (tab/env
  // -filtered) total, which can under/overstate what the search condition
  // alone will match once saved without that tab preset (e.g. it also
  // catches already-acked alerts the "Alerts" tab hides) — showing a number
  // here would just be a misleading promise.
  // Gate on `searchCondition !== null` too: while the debounced server parse
  // of a fresh keystroke is in flight (or failed), `list.data` still reflects
  // the previously-accepted filter (see SearchBar's cadence contract), so a
  // condition parsed from the newest `searchText` could disagree with what
  // `searchMatchCount` actually counted.
  const searchSnoozeCond = useMemo(() => searchOnlyCondition(searchText), [searchText]);
  const searchMatchCount = list.data?.meta.total ?? 0;
  const searchBadge = useMemo(() => {
    if (!searchSnoozeCond || searchCondition === null || searchMatchCount <= 0) return null;
    return (
      <button
        type="button"
        className={styles.searchSnoozeBadge}
        onClick={() => snoozeFromCondition(searchSnoozeCond, searchText)}
        title="Snooze the alerts matching this search"
      >
        <Badge variant="info">Snooze</Badge>
      </button>
    );
  }, [searchSnoozeCond, searchCondition, searchMatchCount, searchText, snoozeFromCondition]);
  const searchProp = useMemo(
    () => ({
      value: searchText,
      onChange: handleSearchChange,
      onSubmit: handleSearchSubmit,
      collection: "record",
      endSlot: searchBadge,
    }),
    [searchText, handleSearchChange, handleSearchSubmit, searchBadge],
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
      <SavedSearches currentQuery={searchText} onApply={handleSearchSubmit} />
      {hasChipFilters ? (
        <ActiveFilters
          tab={activeTab}
          envs={selectedEnvs}
          envName={envName}
          onRemoveEnv={removeEnv}
          onClearTab={clearTab}
          onClearAll={clearAllFilters}
        />
      ) : null}
      <div id="alerts-panel" role="tabpanel" aria-labelledby={`alerts-tab-${activeTab}`}>
        <DataTable
          data={filtered}
          columns={columns}
          rowKey={rowKey}
          loading={list.isPending}
          stale={list.isPlaceholderData}
          emptyState={emptyState}
          selectable
          selectedKeys={selectedKeys}
          onSelectionChange={(keys) => {
            setSelectedKeys(keys);
            // Reset selectAllMode when the user manually changes the selection
            // (deselecting a row, or unchecking select-all should exit the
            // "all N matching" scope).
            if (keys.size === 0) setSelectAllMode(false);
          }}
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
            <>
              <IconButton
                icon="refresh"
                label="Refresh alerts"
                size="sm"
                loading={list.isFetching}
                onClick={() => void list.refetch()}
              />
              <Tooltip
                content={
                  !auto.enabled
                    ? "Auto-refresh off"
                    : refreshPaused
                      ? "Auto-refresh paused while the detail drawer is open"
                      : `Auto-refresh every ${Math.round(refreshMs / 1000)}s`
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
            </>
          }
          serverSort={serverSort}
          serverPagination={{
            page,
            pageSize: PAGE_SIZE,
            total: list.data?.meta.total ?? 0,
            onChange: handlePageChange,
          }}
          rowActions={rowActions}
          rowActionsBadge={rowActionsBadge}
          quickActions={quickActions}
          rowKeyBindings={rowKeyBindings}
          keyboardHints={ALERT_KEYBOARD_HINTS}
          rowAccent={rowAccent}
          contextMenuItems={contextMenuItems}
          renderDetails={renderDetails}
          detailsTitle={detailsTitle}
          // Controlled detail drawer: the open record lives in the URL
          // (?record=), so it's shareable/deep-linkable and survives reloads.
          // A ?record= uid that isn't on the current page clears itself once
          // loading settles (DataTable's auto-close). The "View details" kebab
          // item, the hover-revealed eye icon, and the `E` shortcut all route
          // through here (row click intentionally does not open the drawer).
          detailsKey={record ?? null}
          onDetailsKeyChange={handleDetailsKeyChange}
        />
      </div>
      {dialog ? (
        <ActionDialog
          open
          onOpenChange={(o) => {
            if (!o) {
              setDialog(null);
              setSelectAllMode(false);
            }
          }}
          actionType={dialog.type}
          records={dialog.records}
          onConfirm={submitDialog}
          submitting={bulkSubmitting}
        />
      ) : null}
      <BulkTagDialog
        open={bulkTagOpen}
        onOpenChange={(o) => setBulkTagOpen(o)}
        q={selectAllMode ? q : encodeUidsAsQ([...selectedKeys])}
        recordCount={selectAllMode ? (list.data?.meta.total ?? 0) : selectedKeys.size}
      />
      <ConfirmDeleteDialog
        state={confirmDelete.state}
        onCancel={confirmDelete.cancel}
        onConfirm={() => void confirmDelete.confirm()}
      />
      <ShelveDialog
        open={shelveDialog !== null}
        records={shelveDialog ?? []}
        onOpenChange={(o) => {
          if (!o) setShelveDialog(null);
        }}
        submitting={commentMut.isPending}
        onConfirm={async ({ duration, message }) => {
          if (!shelveDialog) return;
          try {
            for (const r of shelveDialog) {
              await commentMut.mutateAsync({
                record_uid: r.uid ?? "",
                type: "shelve",
                ...(duration ? { duration } : {}),
                ...(message ? { message } : {}),
              });
            }
            const label =
              shelveDialog.length === 1
                ? recordLabel(shelveDialog[0]!)
                : `${shelveDialog.length} alerts`;
            toast.undo(`Shelved • ${label}`, () => {
              void (async () => {
                try {
                  for (const r of shelveDialog) {
                    await commentMut.mutateAsync({ record_uid: r.uid ?? "", type: "unshelve" });
                  }
                } catch (e) {
                  const detail = e instanceof ApiError ? e.detail : "Undo failed";
                  toast.error(detail);
                }
              })();
            });
          } catch (e) {
            const detail = e instanceof ApiError ? e.detail : "Action failed";
            toast.error(detail);
          } finally {
            setShelveDialog(null);
          }
        }}
      />
      <InjectAlertsDialog open={injectOpen} onOpenChange={setInjectOpen} />
      <Dialog
        open={snoozeBulkConfirm !== null}
        onOpenChange={(o) => {
          if (!o) setSnoozeBulkConfirm(null);
        }}
      >
        <DialogContent>
          <DialogTitle>Snooze {snoozeBulkConfirm?.length ?? 0} alerts?</DialogTitle>
          <DialogBody>
            <p>
              This creates one snooze rule matching ANY of these alerts (their host+message
              conditions OR&apos;d together) — worth a quick glance before combining that many
              into a single rule:
            </p>
            {snoozeBulkConfirm
              ? (() => {
                  const { lines, more } = bulkSnoozePreview(snoozeBulkConfirm);
                  return (
                    <ul className={styles.snoozePreviewList}>
                      {lines.map((line, i) => (
                        <li key={i}>{line}</li>
                      ))}
                      {more > 0 ? <li>+{more} more</li> : null}
                    </ul>
                  );
                })()
              : null}
          </DialogBody>
          <DialogFooter>
            <Button variant="secondary" onClick={() => setSnoozeBulkConfirm(null)}>
              Cancel
            </Button>
            <Button
              variant="primary"
              leadingIcon="moon"
              onClick={() => {
                if (snoozeBulkConfirm) snoozeRows(snoozeBulkConfirm);
                setSnoozeBulkConfirm(null);
              }}
            >
              Continue
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
