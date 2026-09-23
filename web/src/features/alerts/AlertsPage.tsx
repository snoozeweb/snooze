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
import { describeActionError, describeError, type ErrorCopy } from "@/lib/api/errorMessage";
import { copyToClipboard } from "@/lib/clipboard";
import { expandTemplate } from "@/lib/clipboard-template";
import { ConfirmDeleteDialog, useConfirmDelete } from "@/shared/ui/resourceContextMenu";
import { encodeConditionQ } from "@/lib/condition/serialize";
import { parseText } from "@/lib/condition/text";
import type { Condition } from "@/lib/condition/types";
import type { ParsedCondition } from "@/shared/ui/SearchBar";
import { severityToken } from "@/lib/format/severity-color";
import { trimDate } from "@/lib/format/time";
import { Icon } from "@/shared/icons/Icon";
import { useConsoleConfig } from "@/features/config/api";
import { useStats } from "@/features/dashboard/api";
import { presetToRange } from "@/features/dashboard/time-range";
import { usePublishPaletteActions, type PaletteAction } from "@/shared/hooks/usePaletteActions";
import { useAnnounce } from "@/shared/a11y/LiveAnnouncer";
import { Environments } from "@/features/admin/environments/api";
import type { IconName } from "@/shared/icons/icon-names";
import {
  Records,
  useCommentRecord,
  useShelveRecord,
  useBulkStateRecord,
  encodeUidsAsQ,
} from "./api";
import { AlertRowDetail, DiscardAnalysisDraftDialog } from "./AlertRowDetail";
import { AlertFlowChart } from "./AlertFlowChart";
import { ActiveFilters } from "./ActiveFilters";
import { AlertsFilters, type AlertFilters } from "./Filters";
import { SavedSearches } from "./SavedSearches";
import { alertColumns, recordCommentCount } from "./columns";
import { useAutoRefresh } from "./useAutoRefresh";
import type { Record_, AlertState } from "./types";
import { tabById, type TabId } from "./tabs";
import { ActionDialog, type ActionType } from "./ActionDialog";
import { ShelveDialog } from "./ShelveDialog";
import { ROW_ACTION_DESCRIPTIONS, SILENCE_DESCRIPTIONS } from "./silencingGuide";
import { BulkTagDialog } from "./BulkTagDialog";
import { InjectAlertsDialog } from "./InjectAlertsDialog";
import {
  isActionAllowed,
  validBulkStates,
  eligibleForBulkState,
  describeBulkSkips,
  BULK_STATE_CAVEAT,
} from "./transitions";
import styles from "./AlertsPage.module.css";

// Module-scope constant so the reference is stable across renders (DataTable's
// row memo depends on stable rowActions identity). comment/shelve are always-
// allowed and appended unconditionally after the filtered set.
const CANDIDATE_ROW_ACTIONS: Array<{ key: ActionType; label: string; icon: IconName }> = [
  { key: "ack", label: "Acknowledge", icon: "thumbs-up" },
  { key: "close", label: "Close", icon: "check-circle" },
  { key: "esc", label: "Re-escalate", icon: "rotate-cw" },
  { key: "open", label: "Re-open", icon: "rotate-cw" },
];

// The two lifecycle verbs the bulk bar always offers (see `stateButton`).
// `pastVerb` is the participle the refusal tooltip needs ("…can be closed").
//
// Weighting: red is reserved for the irreversible — Delete, and nothing else.
// Close used to wear it, which put a routine, reversible triage verb in the
// same paint as data loss AND in the same paint as a critical severity badge.
// Closing is undoable from two directions (Re-open in the kebab, the Undo
// toast on the inline path), so it is a plain secondary now. Acknowledge takes
// the single filled button instead: it is the verb this bar exists for and the
// one an operator reaches for most, and one primary means one focal point.
const BULK_STATE_META: Record<
  "ack" | "close",
  { label: string; icon: IconName; variant: "primary" | "secondary"; pastVerb: string }
> = {
  ack: { label: "Acknowledge", icon: "thumbs-up", variant: "primary", pastVerb: "acknowledged" },
  close: { label: "Close", icon: "check-circle", variant: "secondary", pastVerb: "closed" },
};

/** What a partially-eligible bulk action is leaving behind: how many rows, why,
 *  and out of how big a selection. Carried into the confirm dialog and the
 *  result toast so the skipped rows are stated at both ends. */
type BulkSkip = { count: number; reason: string; selected: number };

/** "1 alert" / "4 alerts" — the live-region copy reads the count out loud, so
 *  the noun has to agree with it. */
function alertCount(n: number): string {
  return `${n} alert${n === 1 ? "" : "s"}`;
}

/** Short human label for a record used in undo-toast copy ("Acknowledged X"). */
function recordLabel(r: Record_): string {
  return r.host ?? r.message ?? r.uid ?? "alert";
}

/** Verb form for describeActionError's "Couldn't <verb> <subject>" copy —
 *  mirrors ActionDialog's META titles/confirmLabels for the same actionType. */
const ACTION_VERB: Record<ActionType, string> = {
  ack: "acknowledge",
  close: "close",
  open: "re-open",
  esc: "re-escalate",
  comment: "comment on",
};

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
   * Open the inspector on the Analysis tab instead of Timeline. Set by deep
   * links whose subject is the analysis rather than the alert — the dashboard's
   * Analyses panel is the one producer. Meaningless without `record`, and
   * dropped together with it when the drawer closes.
   */
  analysis?: boolean | "1";
  /**
   * SearchBar DSL text. Seeds local state on mount and re-seeds on external
   * URL changes (browser nav, deep-links such as the host hyperlink in Teams
   * alert cards: `/web/alerts?search=hash%20%3D%20<hash>`). It is *not* written
   * per-keystroke (see the note below) — only on a discrete commit (Enter /
   * clear), handled by `handleSearchSubmit`.
   */
  search?: string;
};

/**
 * Whether `?analysis=` asks for the Analysis tab.
 *
 * It takes `unknown` because search params arrive from a URL a human or
 * another product can type: the router hands back `true` for the `analysis=true`
 * the dashboard's Link writes, but `1` or `"1"` for a hand-written one, and all
 * three mean the same thing. Anything else (including an explicit `false`) is
 * the default Timeline open.
 */
function wantsAnalysisTab(value: unknown): boolean {
  return value === true || value === "1" || value === 1 || value === "true";
}

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
// bindings wired in `rowKeyBindings` (a=ack, c=close, m=comment); the table
// prepends its own built-in navigation shortcuts (move / open / view / expand /
// select). Module constant so its identity is stable across renders (row-memo
// contract).
const ALERT_KEYBOARD_HINTS = [
  { keys: "A", label: "Acknowledge focused alert" },
  { keys: "C", label: "Close focused alert" },
  { keys: "M", label: "Comment on focused alert" },
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

/** "This condition filters nothing", in either of the two shapes above. */
function isEmptyCondition(dsl: ParsedCondition | Condition): boolean {
  const op = (dsl as ParsedCondition).op;
  if (op !== undefined) return op === "" || op === "ALWAYS_TRUE";
  return (dsl as Condition).type === "ALWAYS_TRUE";
}

function buildQueryParam(
  tab: TabId,
  dsl: ParsedCondition | Condition | null,
  envCondition: Condition | null,
): string | undefined {
  const parts: Condition[] = [];
  const tabCondition = tabById(tab).condition;
  if (tabCondition) parts.push(tabCondition);
  // Two shapes arrive here and both are forwarded verbatim: the backend's own
  // wire form (`op`/`children`) from the SearchBar's server parse, and the
  // frontend `Condition` (`type`/`args`) from the URL seed — Go's
  // UnmarshalJSON normalises either into the canonical form. "Nothing to
  // filter by" is spelled differently in each, hence both guards.
  if (dsl !== null && !isEmptyCondition(dsl)) {
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
  const [dialog, setDialog] = useState<{
    type: ActionType;
    records: Record_[];
    /** Set when the bulk bar narrowed `records` to the eligible subset. */
    skip?: BulkSkip;
  } | null>(null);
  // Failure from the most recent submitDialog attempt, rendered inline inside
  // the ActionDialog (see the corner-toast-only bug this replaces). Cleared
  // whenever a fresh dialog opens or a new submit attempt starts.
  const [dialogError, setDialogError] = useState<ErrorCopy | null>(null);
  const [shelveDialog, setShelveDialog] = useState<Record_[] | null>(null);
  // Failure from the most recent shelve attempt — same inline-error, stay-open
  // pattern as `dialogError`/ActionDialog (see ShelveDialog's `error` prop).
  const [shelveDialogError, setShelveDialogError] = useState<ErrorCopy | null>(null);
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
      // The condition rides along, for the same reason it is seeded at mount:
      // a back/forward step or a second deep link clicked while this page is
      // mounted must filter on the new query immediately, not one server parse
      // later — long enough for a deep-linked drawer to be closed as stale.
      setSearchCondition(searchOnlyCondition(search.search ?? ""));
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

  // The condition the list is actually filtered by. The SearchBar owns the
  // authoritative parse (a debounced POST /condition/parse), but a deep link
  // arrives with its query already in the URL, and waiting a round-trip for it
  // meant the FIRST fetch ran unfiltered — page 1 of the newest alerts, which
  // is exactly where a deep-linked `?record=` uid is not. DataTable then
  // auto-closed the drawer as a stale link and pushed a `?record=`-less URL
  // over it: the double navigation that made a dashboard Analyses row open on
  // no inspector and left a back button that went nowhere useful.
  //
  // So seed it synchronously from `?search=` with the local parser — the same
  // DSL, the same AST the backend normalises — and let the SearchBar's answer
  // replace it when it lands. `parseText` failing just means no seed: the
  // unfiltered first fetch is the old behaviour, not a new failure mode.
  const [searchCondition, setSearchCondition] = useState<ParsedCondition | Condition | null>(() =>
    searchOnlyCondition(search.search ?? ""),
  );
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
    (next: Partial<AlertsSearch>, opts?: { replace?: boolean }) => {
      // TanStack Router's navigate types are locked to the registered route tree at
      // build time. Casting through unknown avoids the "unsafe call" lint issue while
      // still satisfying the type checker when the route is fully registered.
      type NavigateFn = (opts: {
        to: string;
        search: (prev: AlertsSearch | undefined) => AlertsSearch;
        replace?: boolean;
      }) => Promise<void>;
      void (navigate as unknown as NavigateFn)({
        to: "/web/alerts",
        search: (prev: AlertsSearch | undefined) => ({ ...(prev ?? {}), ...next }),
        // `replace` is for the page correcting ITSELF — stripping a parameter
        // that cannot apply, closing a drawer whose row was never here. Those
        // are not places an operator asked to be, so Back must skip them
        // rather than land on a URL this page will immediately correct again.
        ...(opts?.replace ? { replace: true } : {}),
      });
    },
    [navigate],
  );

  // Open detail record (drives the modal detail drawer, synced to the URL as
  // ?record=). Undefined = no drawer open.
  const record = search.record;
  // ?analysis= rides along with ?record= and only picks the tab the inspector
  // opens on.
  const openOnAnalysis = wantsAnalysisTab(search.analysis);
  // `analysis` describes how an inspector OPENS, so it is meaningless on its
  // own. Left in the URL without a `record` — a hand-edited link, or a close
  // that raced the navigation — it silently retargets the next plain row click
  // to the Analysis tab. Strip it as soon as it is on its own.
  useEffect(() => {
    if (search.record === undefined && search.analysis !== undefined) {
      updateSearch({ analysis: undefined } as unknown as Partial<AlertsSearch>, { replace: true });
    }
  }, [search.record, search.analysis, updateSearch]);

  // Whether the inspector currently has an analysis editor open, and the
  // retarget waiting on the answer. AlertRowDetail reports the first; the
  // second is the key prev/next asked for and did not get.
  const [detailEditing, setDetailEditing] = useState(false);
  const [pendingDetailsKey, setPendingDetailsKey] = useState<string | null>(null);

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

  // Did the open `?record=` ever name a row on this page? DataTable closes a
  // deep-linked key that never resolved (a stale link, another filter's uid),
  // and that close comes back through the same `onDetailsKeyChange(null)` an
  // operator's own close does. The difference is whether the drawer was ever
  // really open: a close of something that never opened is a correction, and
  // corrections replace rather than push — otherwise the URL the operator
  // arrived on stays one Back away, gets corrected again, and the button stops
  // going anywhere.
  const recordResolvedRef = useRef(false);
  useEffect(() => {
    if (record === undefined) {
      recordResolvedRef.current = false;
      return;
    }
    if ((list.data?.data ?? []).some((r) => recordKey(r) === record)) {
      recordResolvedRef.current = true;
    }
  }, [record, list.data]);

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

  // Screen-reader parity for the same poll tick. A sighted operator watches the
  // toolbar count change under them; without this, everyone else gets a table
  // that silently rewrites itself. Deliberately a separate baseline from the
  // audio cue's: this one resets whenever the *question* changes (tab, env,
  // search, sort, page) so a filter the operator just applied is not reported
  // back to them as if the server had pushed it — they already got feedback
  // from the control they touched.
  const announce = useAnnounce();
  const queryIdentity = `${q ?? ""}|${page}|${orderby}|${asc ? "asc" : "desc"}`;
  const announceKeyRef = useRef<string | null>(null);
  const announceTotalRef = useRef<number>(-1);
  useEffect(() => {
    if (!list.data) return;
    // The resource keeps the previous page's data on screen while a new query
    // key loads (placeholderData: keepPreviousData). That row of numbers
    // belongs to the *old* question, so pairing it with the new queryIdentity
    // would rebaseline against a stale total and then report the difference as
    // if the server had pushed it.
    if (list.isPlaceholderData) return;
    const total = list.data.meta.total;
    const sameQuestion = announceKeyRef.current === queryIdentity;
    const baseline = announceTotalRef.current;
    announceKeyRef.current = queryIdentity;
    announceTotalRef.current = total;
    // First data for this query — nothing to compare against, and a first load
    // is something the operator asked for anyway.
    if (!sameQuestion || baseline < 0) return;
    // Same guards as the audio cue: silent when the operator turned polling off
    // or a detail drawer froze it.
    if (!auto.enabled || refreshPaused) return;
    if (total === baseline) return;
    const delta = Math.abs(total - baseline);
    const direction = total > baseline ? "new" : "fewer";
    announce(`Alerts refreshed. ${alertCount(total)}, ${delta} ${direction}.`);
  }, [list.data, list.isPlaceholderData, queryIdentity, auto.enabled, refreshPaused, announce]);

  // The Switch announces its own checked state, which says nothing about what
  // it controls. Spell out the consequence for the list instead.
  // Destructured because useAutoRefresh returns a fresh object each render;
  // the setter itself is stable, so this keeps the handler identity stable too.
  const { setEnabled: setAutoRefresh } = auto;
  const handleAutoToggle = useCallback(
    (next: boolean) => {
      setAutoRefresh(next);
      announce(next ? "Auto-refresh on" : "Auto-refresh off");
    },
    [setAutoRefresh, announce],
  );

  // A failed fetch leaves the table showing the last good page, so nothing
  // moves and the refresh button looks broken. Say so instead: a toast on the
  // click the operator made, and a standing badge for as long as the polls
  // keep failing.
  const listErrorCopy = describeError(
    list.error,
    "Couldn't reach the server — the list below may be stale.",
  );
  const refreshErrorDetail = listErrorCopy.summary;
  // Wall-clock of the last response that actually carried rows. `0` means the
  // query has never succeeded in this session — the difference between "stale"
  // and "we never got anything", which the copy below states outright.
  const lastLoadedAt =
    list.dataUpdatedAt > 0 ? trimDate(Math.floor(list.dataUpdatedAt / 1000)) : null;
  const handleManualRefresh = useCallback(async () => {
    const { isError, error } = await list.refetch();
    if (!isError) return;
    toast.error(describeError(error, "Couldn't refresh alerts").summary);
  }, [list]);

  // Memoized so the empty-array fallback keeps one identity across renders —
  // several hooks below (selectedRows, the palette actions) depend on it.
  const filtered = useMemo(() => list.data?.data ?? [], [list.data]);

  const confirmDelete = useConfirmDelete<Record_>({
    onDelete: (uid) => removeMut.mutateAsync(uid),
    noun: "alert",
    // Keep only failed rows selected so a retry targets exactly them.
    onAfter: (failed) => setSelectedKeys(new Set(failed.map((r) => r.uid ?? "").filter(Boolean))),
  });

  const openDialog = useCallback((type: ActionType, records: Record_[]) => {
    setDialogError(null);
    setDialog({ type, records });
  }, []);

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
    [
      snoozeRows,
      snoozeFromCondition,
      searchCondition,
      searchText,
      selectAllMode,
      list.data?.meta.total,
    ],
  );

  const rowActions = useCallback(
    (row: Record_): RowAction[] => {
      const state = (row.state ?? "") as AlertState;
      const isClosed = state === "close";
      // ttl<0 branch: legacy permanent-exempt rows (pre-plan-34b); new-model rows use state=="shelved"
      const isShelved = state === "shelved" || (row.ttl !== undefined && row.ttl < 0);

      // Grouped with headings and separators (see RowActionHeading /
      // RowActionSeparator): lifecycle state changes, then the always-available
      // engage actions, then the ways to make it stop bothering you. A flat
      // 7-item list reads as one undifferentiated pile — the grouping mirrors
      // how an operator actually thinks about the menu ("what state should this
      // be in" vs. "how do I quiet it down"), and the headings say so out loud
      // instead of leaving a bare rule to imply it.
      const out: RowAction[] = [];

      // ── Change state ─────────────────────────────────────────────────
      // Flat filter over candidates using the transition gate — replaces the
      // nested if (isOpen) / else if (isAcked) / else if (isClosed) chains.
      const lifecycle: RowAction[] = [];
      for (const { key, label, icon } of CANDIDATE_ROW_ACTIONS) {
        if (isActionAllowed(state, key)) {
          lifecycle.push({
            key,
            label,
            icon,
            ...(ROW_ACTION_DESCRIPTIONS[key] ? { description: ROW_ACTION_DESCRIPTIONS[key] } : {}),
            onSelect: () => openDialog(key, [row]),
          });
        }
      }
      // A heading over nothing is worse than no heading: some states allow no
      // transition at all, and the group is omitted entirely then.
      if (lifecycle.length > 0) {
        out.push({ key: "head-state", heading: "Change state" });
        out.push(...lifecycle);
        out.push({ key: "sep-lifecycle", separator: true });
      }

      // ── Engage ───────────────────────────────────────────────────────
      // comment is always-allowed.
      out.push({ key: "head-engage", heading: "Engage" });
      out.push({
        key: "comment",
        label: "Comment",
        icon: "message-square",
        onSelect: () => openDialog("comment", [row]),
      });

      // ── Quiet it down ────────────────────────────────────────────────
      // Snooze lives here rather than beside Comment: it is one of the four
      // ways to make an alert stop bothering you, and filing it under
      // "Engage" hid that. It stays ungated — a snooze is a preventive rule
      // about future alerts, not a transition on this record, so it is offered
      // even for closed rows (the shelve verbs below are not).
      out.push({ key: "sep-suppress", separator: true });
      out.push({ key: "head-quiet", heading: "Quiet it down" });
      out.push({
        key: "snooze",
        label: "Snooze this alert",
        icon: "moon",
        description: SILENCE_DESCRIPTIONS.snooze,
        onSelect: () => snoozeRows([row]),
      });

      if (!isClosed) {
        if (isShelved) {
          // A row can be hidden by EITHER mechanism, and the two are
          // independent: `state=="shelved"` (timed shelve, a comment) and
          // `ttl<0` (permanent shelve, a PATCH). Undoing only the one you can
          // see leaves the row hidden — the bug where "Unshelve" on a
          // permanently-shelved alert posted a comment, flipped state to
          // "open", and changed nothing the operator could observe, because
          // the Shelved tab and the default Alerts preset both filter on
          // `ttl < 0` too (see tabs.ts). So reverse whichever ones are set,
          // and both when both are.
          const wasStateShelved = state === "shelved";
          const wasPermanent = row.ttl !== undefined && row.ttl < 0;
          out.push({
            key: "unshelve",
            label: "Unshelve",
            icon: "eye",
            description: SILENCE_DESCRIPTIONS.unshelve,
            onSelect: () => {
              void (async () => {
                const uid = row.uid ?? "";
                try {
                  if (wasStateShelved) {
                    await commentMut.mutateAsync({ record_uid: uid, type: "unshelve" });
                  }
                  if (wasPermanent) {
                    await shelveMut.mutateAsync({ uid, shelve: false, currentTTL: row.ttl });
                  }
                  toast.undo(`Unshelved • ${recordLabel(row)}`, () => {
                    void (async () => {
                      try {
                        if (wasStateShelved) {
                          await commentMut.mutateAsync({ record_uid: uid, type: "shelve" });
                        }
                        if (wasPermanent) {
                          // The unshelve above flipped the ttl back to its
                          // positive magnitude, so that positive value is what
                          // the re-shelve must negate to land on the original.
                          await shelveMut.mutateAsync({
                            uid,
                            shelve: true,
                            currentTTL: row.ttl === undefined ? undefined : -row.ttl,
                          });
                        }
                      } catch (e) {
                        toast.error(describeError(e, "Undo failed").summary);
                      }
                    })();
                  });
                } catch (e) {
                  toast.error(describeError(e, "Action failed").summary);
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
            description: SILENCE_DESCRIPTIONS.shelve,
            onSelect: () => {
              setShelveDialogError(null);
              setShelveDialog([row]);
            },
          });
          // Legacy code path (ttl=-1) for a shelve that never expires — same
          // outcome as "Shelve" above minus the timer, so it's named as its
          // permanent counterpart rather than exposing the implementation's
          // vintage to the operator.
          out.push({
            key: "permanent-exempt",
            label: "Shelve permanently",
            icon: "eye-off",
            description: SILENCE_DESCRIPTIONS.shelveForever,
            onSelect: () => {
              void (async () => {
                try {
                  await shelveMut.mutateAsync({
                    uid: row.uid ?? "",
                    shelve: true,
                    currentTTL: row.ttl,
                  });
                  toast.undo(`Shelved permanently • ${recordLabel(row)}`, () => {
                    void (async () => {
                      try {
                        await shelveMut.mutateAsync({
                          uid: row.uid ?? "",
                          shelve: false,
                          currentTTL: row.ttl,
                        });
                      } catch (e) {
                        toast.error(describeError(e, "Undo failed").summary);
                      }
                    })();
                  });
                } catch (e) {
                  toast.error(describeError(e, "Action failed").summary);
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
                toast.error(describeError(e, "Undo failed").summary);
              }
            })();
          });
        } catch (e) {
          toast.error(describeError(e, "Action failed").summary);
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
          // Labelled (not just an icon) in the one place this list feeds a
          // header with room for it — the detail drawer's toolbar — and the
          // filled one of the pair: it is the verb an operator reaches for
          // most, so it gets the header's single focal point.
          emphasize: true,
          primary: true,
          onSelect: () => inlineAction(row, "ack"),
        });
      }
      if (isActionAllowed(state, "close")) {
        out.push({
          key: "close",
          // "Close alert", not "Close": in the detail drawer this button sits
          // a few pixels from the panel's own ✕ (aria-label "Close panel"),
          // and one of those two dismisses a panel while the other ends an
          // alert's triage life. The bulk bar and the kebab keep the bare
          // "Close" — nothing named "Close" is adjacent there.
          label: "Close alert",
          icon: "check-circle",
          emphasize: true,
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
  //   a → Acknowledge the focused row (confirm dialog)
  //   c → Close the focused row (confirm dialog)
  //   m → Comment on the focused row
  // Each opens the SAME ActionDialog the kebab and bulk bar use. The mouse
  // quick-actions still ack/close inline with an Undo toast — a click lands on
  // a row the operator is pointing at, whereas a keystroke lands on whichever
  // row the focus ring happens to be on, so the keyboard path keeps its
  // confirm. `e` (details), `f` (flow) and Space (select) are DataTable's own.
  // Bindings only fire when a row is focused and the user isn't typing.
  const rowKeyBindings = useCallback(
    (row: Record_): Record<string, () => void> => {
      const state = (row.state ?? "") as AlertState;
      const bindings: Record<string, () => void> = {
        m: () => openDialog("comment", [row]),
      };
      if (isActionAllowed(state, "ack")) bindings.a = () => openDialog("ack", [row]);
      if (isActionAllowed(state, "close")) bindings.c = () => openDialog("close", [row]);
      return bindings;
    },
    [openDialog],
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

      // Flat filter over candidates using the transition gate. Close is not
      // danger-marked here or anywhere else — see BULK_STATE_META for why red
      // now belongs to Delete alone.
      for (const { key, label, icon } of CANDIDATE_ROW_ACTIONS) {
        if (isActionAllowed(state, key)) {
          items.push({
            key,
            label,
            icon,
            ...(ROW_ACTION_DESCRIPTIONS[key] ? { description: ROW_ACTION_DESCRIPTIONS[key] } : {}),
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
        description: SILENCE_DESCRIPTIONS.snooze,
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
      const openBulkDialog = (type: ActionType, records: Record_[] = rows, skip?: BulkSkip) => {
        setDialogError(null);
        setDialog({ type, records, ...(skip ? { skip } : {}) });
      };
      const total = list.data?.meta.total ?? 0;
      const pageCount = rows.length;

      // Intersection of valid moves across the selection. Now only gates the
      // two rarer verbs (Re-escalate / Re-open) — Acknowledge and Close are
      // always rendered by `stateButton` below with their own eligibility
      // count. When selectAllMode is true the off-page states are unknown, so
      // every state button is offered and the backend does the filtering.
      const valid = selectAllMode
        ? new Set<ActionType>(["ack", "close", "open", "esc"])
        : validBulkStates(rows);

      // Label suffix: show "all N" when operating on the full query scope.
      const countLabel = selectAllMode ? `all ${total}` : String(pageCount);

      // Acknowledge and Close are the two verbs the whole page exists for, so
      // they are ALWAYS on the bar. They used to disappear whenever a single
      // selected row disagreed (the intersection above) — select-all on the
      // "All" tab left an operator with Comment/Tag/Snooze and no explanation.
      // Now they carry their own eligibility count, act on the eligible subset,
      // and say why when nothing qualifies.
      const stateButton = (action: "ack" | "close") => {
        const meta = BULK_STATE_META[action];
        if (selectAllMode) {
          // Off-page states are unknowable from here; the backend applies the
          // same transition table per record, so the scope is the whole query.
          return (
            <Button
              size="sm"
              variant={meta.variant}
              leadingIcon={meta.icon}
              onClick={() => openBulkDialog(action)}
            >
              {meta.label} ({countLabel})
            </Button>
          );
        }
        const eligible = eligibleForBulkState(rows, action);
        const skipped = pageCount - eligible.length;
        const reason = describeBulkSkips(rows, action);
        const count = skipped > 0 ? `${eligible.length} of ${pageCount}` : String(pageCount);
        if (eligible.length === 0) {
          // aria-disabled rather than `disabled`: the button stays focusable,
          // so the tooltip that explains the refusal is reachable by keyboard
          // instead of being a mouse-only courtesy.
          return (
            <Tooltip content={`None of the selected alerts can be ${meta.pastVerb} — ${reason}.`}>
              <Button
                size="sm"
                variant={meta.variant}
                leadingIcon={meta.icon}
                aria-disabled
                onClick={() => undefined}
              >
                {meta.label} ({count})
              </Button>
            </Tooltip>
          );
        }
        return (
          <Tooltip
            content={
              skipped > 0
                ? `${skipped} of the ${pageCount} selected will be skipped (${reason}).`
                : null
            }
          >
            <Button
              size="sm"
              variant={meta.variant}
              leadingIcon={meta.icon}
              onClick={() =>
                openBulkDialog(
                  action,
                  eligible,
                  skipped > 0 ? { count: skipped, reason, selected: pageCount } : undefined,
                )
              }
            >
              {meta.label} ({count})
            </Button>
          </Tooltip>
        );
      };

      return (
        <>
          {stateButton("ack")}
          {/* Acknowledge leads and is the only filled button on the bar; Close
              follows as a plain secondary. See BULK_STATE_META for why Close
              gave up its red — it is reversible, and red now means Delete. */}
          {stateButton("close")}
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
      const { type, records, skip } = dialog;
      setDialogError(null);
      // "srv-prod-db-01" for a single record, "3 alerts" for a bulk action —
      // names the object in the inline failure copy below.
      const subject = records.length === 1 ? recordLabel(records[0]!) : `${records.length} alerts`;

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
          // whether to retry — inline in the still-open dialog, not just a
          // toast that can be missed and vanishes with the detail.
          const firstRejected = results.find(
            (r): r is PromiseRejectedResult => r.status === "rejected",
          );
          const { summary, secondary } = describeError(firstRejected?.reason, "Comment failed");
          setDialogError({
            summary: `${failed} of ${records.length} comments failed — ${summary[0]!.toLowerCase()}${summary.slice(1)}`,
            ...(secondary ? { secondary } : {}),
          });
          toast.error(`${failed} of ${records.length} failed; ${ok} succeeded`);
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
        // Rows the bulk bar deliberately left out of the request (an action
        // that was legal for some of the selection but not all of it) are
        // reported here — otherwise "7 alerts updated" after selecting 10
        // reads as three silent failures.
        const skippedNote = skip ? ` — ${skip.count} skipped (${skip.reason})` : "";
        // Description must carry both the count and the caveat because tests
        // assert against t.description.
        const description = `${mainMsg}${partialNote}${skippedNote} — ${BULK_STATE_CAVEAT}`;
        // ack/close both have a real, backend-legal inverse ("open" — see
        // transitions.ts's ALLOWED table), so offer it as an Undo action
        // instead of leaving the operator to hunt for "Re-open" again. esc's
        // "undo" is ambiguous (its prior state could have been ack, esc, or
        // open) and re-open has no single well-defined inverse, so those
        // stay plain success toasts rather than guessing.
        if (type === "ack" || type === "close") {
          toast.undo(description, () => {
            void (async () => {
              try {
                await bulkStateMut.mutateAsync({ ...(bulkQ ? { q: bulkQ } : {}), state: "open" });
              } catch (e) {
                toast.error(describeError(e, "Undo failed").summary);
              }
            })();
          });
        } else {
          toast.success(description);
        }
        setDialog(null);
        setSelectedKeys(new Set());
        setSelectAllMode(false);
      } catch (e) {
        // Inline, named, actionable: rendered inside the still-open dialog
        // (see ActionDialog's `error` prop) so the failure is where the
        // operator is looking, not only in a corner toast. The toast stays as
        // a secondary channel for anyone not looking at the dialog.
        setDialogError(describeActionError(ACTION_VERB[type], subject, e));
        toast.error(describeError(e, "Bulk action failed").summary);
      } finally {
        setBulkSubmitting(false);
      }
    },
    [commentMut, bulkStateMut, dialog, selectAllMode, q],
  );

  // The rows behind the current selection, resolved from the visible page.
  const selectedRows = useMemo(
    () => filtered.filter((r) => selectedKeys.has(recordKey(r))),
    [filtered, selectedKeys],
  );

  // Commands this page contributes to ⌘K. Deliberately only the two verbs an
  // operator reaches the palette for mid-triage — the palette is a shortcut to
  // the bulk bar, not a second copy of it — and only while a selection exists,
  // so the palette stays a jump list the rest of the time.
  const paletteActions = useMemo<PaletteAction[]>(() => {
    if (selectedRows.length === 0) return [];
    const hint = `${selectedRows.length} selected`;
    const out: PaletteAction[] = [];
    if (validBulkStates(selectedRows).has("ack")) {
      out.push({
        id: "alerts-ack-selected",
        label: "Acknowledge selected alerts",
        icon: "thumbs-up",
        hint,
        run: () => openDialog("ack", selectedRows),
      });
    }
    out.push({
      id: "alerts-snooze-selected",
      label: "Snooze selected alerts…",
      icon: "moon",
      hint,
      run: () => requestSnoozeRows(selectedRows),
    });
    return out;
  }, [selectedRows, openDialog, requestSnoozeRows]);
  usePublishPaletteActions(paletteActions);

  // Distinguish a genuinely empty install (no alerts ingested yet) from a
  // filter/search/tab that simply matches nothing. Only the former offers the
  // "how to inject alerts" guidance; the latter nudges the operator to widen
  // their filter. The default "alerts" tab preset does not count as a filter.
  const hasActiveFilters =
    searchText.trim() !== "" ||
    searchCondition !== null ||
    selectedEnvs.length > 0 ||
    activeTab !== "alerts";

  // A cleared queue and a fresh install render identically ("no rows") but
  // deserve opposite copy: a cleared queue earns "All clear" (the operator
  // did the work), a fresh install earns onboarding guidance. `counters`
  // (from /stats) tells them apart cheaply — `present` is true the moment a
  // single alert has ever been ingested, so it survives the queue being
  // fully emptied. Only queried once there's actually nothing to show.
  const isEmptyQueue = !hasActiveFilters && filtered.length === 0 && !list.isPending;
  const everReceivedRange = useMemo(() => presetToRange("1y"), []);
  const everReceivedStats = useStats(
    { ...everReceivedRange, bucket: 86400 },
    { enabled: isEmptyQueue },
  );
  const hasEverReceivedAlert = everReceivedStats.data?.meta.counters?.present ?? false;

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
  // The default (Timeline) is passed as the absent prop rather than spelled
  // out, so the ordinary open keeps rendering exactly what it rendered before.
  const renderDetails = useCallback(
    (row: Record_) =>
      openOnAnalysis ? (
        <AlertRowDetail row={row} defaultTab="analysis" onEditingChange={setDetailEditing} />
      ) : (
        <AlertRowDetail row={row} onEditingChange={setDetailEditing} />
      ),
    [openOnAnalysis],
  );
  // The Flow trace — the one view that answers "why did this fire, and what
  // did it wake up?" — was three interactions deep behind the drawer's Flow
  // tab. Here it is the same component, hung inline under its row, so reading
  // the pipeline path costs one keystroke and never loses the list. The drawer
  // tab stays: it is where Flow sits next to the timeline and the raw record.
  const renderRowExpansion = useCallback((row: Record_) => <AlertFlowChart row={row} />, []);
  // Drawer title: the alert's host in mono (falls back to uid). Host is not
  // repeated in the drawer body, so this is where the operator reads it.
  const detailsTitle = useCallback(
    (r: Record_) => (
      <span className={styles.detailsHost} title={r.host ?? r.uid ?? "alert"}>
        {r.host ?? r.uid ?? "alert"}
      </span>
    ),
    [],
  );
  // Controlled detail drawer: write the open record to the URL (?record=),
  // dropping the key when the drawer closes so deep-links stay clean.
  // `analysis` describes an *opening*, not the page, so it leaves with the
  // drawer: a closed inspector that still carried it would send the next alert
  // opened from the table to the Analysis tab. Retargeting the open drawer
  // (prev/next, another row) keeps it — the inspector's tab state belongs to
  // the reader at that point, not to the link that started them off.
  const applyDetailsKey = useCallback(
    (k: string | null) =>
      updateSearch(
        {
          record: k ?? undefined,
          ...(k === null ? { analysis: undefined } : {}),
        } as unknown as Partial<AlertsSearch>,
        // A close of a drawer that never resolved to a row is the table
        // correcting a stale deep link, not the operator closing anything —
        // see `recordResolvedRef`.
        k === null && !recordResolvedRef.current ? { replace: true } : undefined,
      ),
    [updateSearch],
  );
  // A retarget swaps the inspector's subject under whatever is open in it. The
  // analysis editor is the one surface that holds unsaved work AND writes it
  // back addressed by the *current* uid, so retargeting past an open one is
  // how a draft written against this alert gets PUT onto the next. Ask first;
  // everything else (a close, a first open) goes straight through.
  const handleDetailsKeyChange = useCallback(
    (k: string | null) => {
      if (k !== null && detailEditing && record !== undefined && k !== record) {
        setPendingDetailsKey(k);
        return;
      }
      applyDetailsKey(k);
    },
    [applyDetailsKey, detailEditing, record],
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

  // A failed list query is NOT an empty queue. Until this existed, a 500 on
  // /record rendered the green "All clear — every alert has been triaged"
  // card: the single most dangerous sentence this product can show, because
  // it is indistinguishable from the good news it imitates. The panel below
  // names the problem, dates the staleness, offers the retry, and keeps the
  // server's own words for the ticket.
  //
  // role="alert" sits on the panel (not on a nested InlineError) so the whole
  // thing is announced once, title first, instead of the raw detail alone.
  const errorState = list.isError ? (
    <div className={styles.errorPanel} role="alert">
      <span className={styles.errorIcon}>
        <Icon name="alert-triangle" size={24} />
      </span>
      <h3 className={styles.errorTitle}>Can&apos;t reach the alert store</h3>
      <p className={styles.errorBody}>
        {lastLoadedAt
          ? `This list is stale as of ${lastLoadedAt}. Alerts may be firing that Snooze can't show you.`
          : "No data loaded — this is a failed request, not an empty queue. Alerts may be firing that Snooze can't show you."}
      </p>
      <p className={styles.errorSummary}>{listErrorCopy.summary}</p>
      {listErrorCopy.secondary && listErrorCopy.secondary !== listErrorCopy.summary ? (
        <p className={styles.errorRaw}>{listErrorCopy.secondary}</p>
      ) : null}
      <div className={styles.errorAction}>
        <Button
          variant="primary"
          leadingIcon="refresh"
          loading={list.isFetching}
          onClick={() => void handleManualRefresh()}
        >
          Try again
        </Button>
      </div>
    </div>
  ) : undefined;

  const emptyState = hasActiveFilters ? (
    <EmptyState
      icon="search"
      title="No alerts match your filters"
      description="Try widening your search, clearing the environment filter, or switching tabs."
    />
  ) : hasEverReceivedAlert ? (
    <EmptyState
      icon="check-circle"
      title="All clear"
      description="Every alert has been triaged. Nothing is waiting on you right now."
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
          errorState={errorState}
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
                onClick={() => void handleManualRefresh()}
              />
              {/* While the polls are failing this is the only thing on screen
                  that contradicts the table, so it gets weight: a red-bordered
                  chip that states the staleness and retries on click, not a
                  quiet pill that reads like metadata. */}
              {list.isError ? (
                <Tooltip content={refreshErrorDetail}>
                  <button
                    type="button"
                    className={styles.staleChip}
                    onClick={() => void handleManualRefresh()}
                  >
                    <Icon name="alert-triangle" size={14} />
                    <span>
                      {lastLoadedAt ? `Not updating since ${lastLoadedAt}` : "Not updating"}
                    </span>
                  </button>
                </Tooltip>
              ) : null}
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
                    onCheckedChange={handleAutoToggle}
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
          // loading settles (DataTable's auto-close). A click on the row body,
          // the "View details" kebab item, the hover-revealed eye icon, `E`
          // and Enter all route through here.
          detailsKey={record ?? null}
          onDetailsKeyChange={handleDetailsKeyChange}
          // Inline Flow expander — the chevron on each row, and `F` on the
          // focused one.
          renderRowExpansion={renderRowExpansion}
          rowExpansionLabel="pipeline flow"
        />
      </div>
      <DiscardAnalysisDraftDialog
        open={pendingDetailsKey !== null}
        onOpenChange={(open) => {
          if (!open) setPendingDetailsKey(null);
        }}
        onDiscard={() => {
          const next = pendingDetailsKey;
          setPendingDetailsKey(null);
          // The drawer keys the details subtree by row, so applying the key
          // remounts it and the editor goes with it.
          setDetailEditing(false);
          if (next !== null) applyDetailsKey(next);
        }}
        description="Moving to another alert closes the editor. Anything you have written here is not saved."
      />
      {dialog ? (
        <ActionDialog
          open
          onOpenChange={(o) => {
            if (!o) {
              setDialog(null);
              setDialogError(null);
              setSelectAllMode(false);
            }
          }}
          actionType={dialog.type}
          records={dialog.records}
          {...(dialog.skip
            ? {
                note: `${dialog.skip.count} of the ${dialog.skip.selected} selected alerts will be skipped (${dialog.skip.reason}).`,
              }
            : {})}
          onConfirm={submitDialog}
          submitting={bulkSubmitting}
          error={dialogError}
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
          if (!o) {
            setShelveDialog(null);
            setShelveDialogError(null);
          }
        }}
        submitting={commentMut.isPending}
        error={shelveDialogError}
        onConfirm={async ({ duration, message }) => {
          if (!shelveDialog) return;
          setShelveDialogError(null);
          const subject =
            shelveDialog.length === 1
              ? recordLabel(shelveDialog[0]!)
              : `${shelveDialog.length} alerts`;
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
                  toast.error(describeError(e, "Undo failed").summary);
                }
              })();
            });
            setShelveDialog(null);
          } catch (e) {
            // Same anti-pattern Phase 3 fixed in ActionDialog: stay open,
            // show the failure inline, let the operator retry instead of
            // silently closing on a failed shelve.
            setShelveDialogError(describeActionError("shelve", subject, e));
            toast.error(describeError(e, "Action failed").summary);
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
              conditions OR&apos;d together) — worth a quick glance before combining that many into
              a single rule:
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
