import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, ReactNode } from "react";
import { Checkbox } from "./Checkbox";
import { EmptyState } from "./EmptyState";
import { Icon } from "@/shared/icons/Icon";
import { IconButton } from "./IconButton";
import { Popover, PopoverContent, PopoverTrigger } from "./Popover";
import { RowActionsMenu, type RowAction } from "./RowActionsMenu";
import { RowDetailsDrawer } from "./RowDetailsDrawer";
import { SearchBar, type ParsedCondition } from "./SearchBar";
import { Skeleton } from "./Skeleton";
import { isEditable } from "@/shared/hooks/useShortcut";
import { DataTableContextMenu, type ContextMenuItem } from "./DataTableContextMenu";
import styles from "./DataTable.module.css";

export type ColumnDef<T> = {
  id: string;
  header: string;
  cell: (row: T) => ReactNode;
  sortable?: boolean;
  align?: "left" | "right";
  width?: string;
  /** Hides the column via a container query when the TABLE'S OWN CONTAINER
   *  (not the viewport — DataTable's wrapper is an inline-size container)
   *  narrows below the tier's breakpoint: "md" 768px, "lg" 1024px, "xl"
   *  1280px, "xxl" 1600px. Tiers are cumulative as the container shrinks —
   *  "xxl" columns (least essential) disappear first, then "xl", then "lg",
   *  then "md" — until card mode kicks in at <=640px and every field
   *  reflows back in as a stacked card, so nothing is ever unreachable. Use
   *  for secondary/derived/metadata columns; identity and primary-status
   *  columns should omit this. */
  hideBelow?: "md" | "lg" | "xl" | "xxl";
};

// Re-exported so existing `import type { RowAction } from "@/shared/ui/DataTable"`
// call sites (many) keep working — the type now lives in RowActionsMenu.tsx.
export type { RowAction };

export type DataTableProps<T> = {
  data: T[];
  columns: ColumnDef<T>[];
  rowKey: (row: T) => string;
  density?: "compact" | "default";
  selectable?: boolean;
  selectedKeys?: ReadonlySet<string>;
  onSelectionChange?: (next: Set<string>) => void;
  serverSort?: {
    sortBy: string;
    order: "asc" | "desc";
    onChange: (next: { sortBy: string; order: "asc" | "desc" }) => void;
  };
  serverPagination?: {
    page: number;
    pageSize: number;
    total: number;
    onChange: (next: { page: number; pageSize: number }) => void;
  };
  rowActions?: (row: T) => RowAction[];
  /** Inline IconButtons revealed on row hover/focus, rendered in their own
   *  column just before the kebab. Hidden by default (opacity), so showing
   *  them never shifts layout, and reachable via :focus-within. The kebab
   *  (`rowActions`) and context menu are unchanged — these are the
   *  one-click shortcuts for the most common per-row operations. */
  quickActions?: (row: T) => RowAction[];
  /** Optional count badge overlaid on the top-right corner of the row-actions
   *  kebab. Returns `{ count, label }` for a row that should carry a pill
   *  (count > 0), or undefined for none. `label` is folded into the kebab's
   *  accessible name (e.g. "Row actions, 2 comments"). Requires `rowActions`. */
  rowActionsBadge?: (row: T) => { count: number; label?: string } | undefined;
  /** Returns a CSS colour (e.g. `var(--severity-critical)`) painted as a 3px
   *  inset strip on the left edge of the row. Undefined → no strip. */
  rowAccent?: (row: T) => string | undefined;
  contextMenuItems?: (row: T) => ContextMenuItem[];
  bulkActions?: (rows: T[]) => ReactNode;
  /** Persistent toolbar rendered above the table. Lives in the same row
   *  as the bulk-actions bar so selecting rows doesn't shift the table
   *  vertically. Pages use this to host their "New" button, refresh
   *  controls, and any other always-visible affordances. */
  toolbar?: ReactNode;
  /** Optional small text rendered at the start of the toolbar (e.g.
   *  "42 users"). When no selection is active it sits next to `toolbar`;
   *  bulk-action mode replaces it with "N selected". */
  toolbarHeader?: ReactNode;
  /** Optional in-table search. When provided, a SearchBar renders above
   *  the table; pages pass through to their resource useList as ?q= for
   *  server-side filtering. Identical surface across every table so
   *  operators get the same DSL everywhere. */
  search?: {
    value: string;
    onChange: (next: { text: string; condition: ParsedCondition | null }) => void;
    /** Committed on Enter / clear — pages persist this to the URL's ?search=. */
    onSubmit?: (text: string) => void;
    /** Field-catalog collection (rule, snooze, user, …) for autocomplete. */
    collection?: string;
    placeholder?: string;
  };
  emptyState?: ReactNode;
  loading?: boolean;
  /** When true the table shows its previous rows while a new query is in
   *  flight (TanStack Query keepPreviousData / placeholderData). The table
   *  dims to signal staleness and sets aria-busy="true". */
  stale?: boolean;
  onRowOpen?: (row: T) => void;
  /** When true for a row, the row renders with muted styling — used to
   *  indicate `enabled:false` records without dedicating a column. */
  rowDisabled?: (row: T) => boolean;
  /** Content of the modal details drawer for a row. When provided, rows get a
   *  "View details" entry in the row-actions kebab, the `E` shortcut, and pages
   *  may open it via their own affordances (e.g. alerts row click). */
  renderDetails?: (row: T) => ReactNode;
  /** Drawer title for the detail row. Falls back to "Details". */
  detailsTitle?: (row: T) => ReactNode;
  /** Controlled open-record key (null = closed). Omit for uncontrolled. */
  detailsKey?: string | null;
  /** Fires with the row key when the drawer opens / retargets, null on close.
   *  Write channel for controlled mode; notification for uncontrolled. */
  onDetailsKeyChange?: (key: string | null) => void;
  /** Per-row keyboard shortcuts for the focused row, keyed by lowercase
   *  single key (e.g. `{ a: ackFn, c: commentFn }`). Bindings are ignored
   *  while the user is typing into an editable field, and when any modifier
   *  key (Ctrl/Cmd/Alt) is held — those combos are reserved for browser
   *  actions (Ctrl+C copy, Ctrl+A select-all) and the global shortcut
   *  registry. Reserved unmodified keys (arrows, j/k, e, x, Enter) are
   *  handled by the table itself and take precedence. */
  rowKeyBindings?: (row: T) => Record<string, () => void>;
  /** Page-supplied row keyboard bindings to advertise, e.g.
   *  `[{ keys: "A", label: "Acknowledge" }]`. When provided, the toolbar shows
   *  a "?" affordance that opens a legend combining these with the table's own
   *  built-in navigation shortcuts (move / open / view / select). Opt-in:
   *  tables that don't pass this get no legend, so the affordance only appears
   *  where per-row shortcuts actually exist (e.g. the Alerts table). */
  keyboardHints?: { keys: string; label: string }[];
};

export function DataTable<T>({
  data,
  columns,
  rowKey,
  density = "default",
  selectable = false,
  selectedKeys,
  onSelectionChange,
  serverSort,
  serverPagination,
  rowActions,
  rowActionsBadge,
  quickActions,
  rowAccent,
  contextMenuItems,
  bulkActions,
  toolbar,
  toolbarHeader,
  search,
  emptyState,
  loading = false,
  stale = false,
  onRowOpen,
  rowDisabled,
  renderDetails,
  detailsTitle,
  detailsKey,
  onDetailsKeyChange,
  rowKeyBindings,
  keyboardHints,
}: DataTableProps<T>) {
  const [focusedIndex, setFocusedIndex] = useState<number>(-1);
  // Uncontrolled details store. Ignored when `detailsKey` is supplied — the
  // controlled path reads from the prop and never touches this. A single
  // string|null: the key of the row whose detail drawer is open, or null.
  const [detailsInner, setDetailsInner] = useState<string | null>(null);
  const isControlledDetails = detailsKey !== undefined;
  const activeDetailsKey = isControlledDetails ? detailsKey : detailsInner;
  const [ctxMenu, setCtxMenu] = useState<{
    row: T;
    index: number;
    x: number;
    y: number;
    selection: string;
  } | null>(null);
  // Anchor index for shift-click range selection. Set on every plain click
  // of a row's checkbox; consumed when the next click arrives with shift.
  const [anchorIndex, setAnchorIndex] = useState<number | null>(null);

  // Per-row memoization (see DataTableRow below) only pays off when the
  // handlers handed to each row keep a stable identity across internal-state
  // changes (focus moves, selection toggles). The callbacks below therefore
  // read mutable values through refs and use functional setState, so they can
  // be created once with empty deps. A single render-time ref-sync keeps the
  // refs current without resurrecting the callbacks.
  const selSetRef = useRef<ReadonlySet<string>>(new Set<string>());
  const anchorIndexRef = useRef<number | null>(anchorIndex);
  const allKeysRef = useRef<string[]>([]);
  const dataRef = useRef<T[]>(data);
  const focusedIndexRef = useRef<number>(focusedIndex);
  const detailsKeyRef = useRef<string | null | undefined>(detailsKey);
  const detailsInnerRef = useRef<string | null>(detailsInner);
  const onSelectionChangeRef = useRef(onSelectionChange);
  const onDetailsKeyChangeRef = useRef(onDetailsKeyChange);
  const onRowOpenRef = useRef(onRowOpen);
  const rowKeyRef = useRef(rowKey);
  const rowKeyBindingsRef = useRef(rowKeyBindings);
  const isControlledDetailsRef = useRef(isControlledDetails);
  // The table root (role="grid"): closeDetails focuses it so keyboard context
  // returns to the grid when the drawer closes.
  const gridRef = useRef<HTMLTableElement>(null);

  // Details write channel. `null` closes the drawer; a key opens exactly that
  // row (replacing whatever was open). Routes through the controlled/
  // uncontrolled fork so the parent owns the key when it supplies `detailsKey`.
  const setDetailsKey = useCallback((key: string | null) => {
    if (isControlledDetailsRef.current) {
      onDetailsKeyChangeRef.current?.(key);
    } else {
      setDetailsInner(key);
    }
  }, []);

  // Open the drawer on the row at `key`/`index` AND focus that row so the
  // keyboard context (arrows/j/k, per-row bindings) resumes there when the
  // drawer closes. Used by the "View details" kebab item and the `E` shortcut.
  const openDetailsAt = useCallback(
    (key: string, index: number) => {
      setFocusedIndex(index);
      setDetailsKey(key);
    },
    [setDetailsKey],
  );

  // Retarget the open drawer to the row at `index` AND focus it — used by the
  // drawer's prev/next controls and the in-drawer ArrowUp/ArrowDown.
  const retargetDetails = useCallback(
    (index: number) => {
      const row = dataRef.current[index];
      if (!row) return;
      setFocusedIndex(index);
      setDetailsKey(rowKeyRef.current(row));
    },
    [setDetailsKey],
  );

  // Close the drawer and hand keyboard focus back to the grid.
  const closeDetails = useCallback(() => {
    setDetailsKey(null);
    gridRef.current?.focus();
  }, [setDetailsKey]);

  // Surface details-key changes to the parent so it can pause polling, etc.
  // Only the uncontrolled path fires from here — in the controlled path the
  // parent already owns the key and setDetailsKey calls onDetailsKeyChange
  // directly, so re-firing on every render would double-notify.
  // The initial null fire on mount is harmless — consumers treat null as
  // "nothing open", which matches the default state.
  useEffect(() => {
    if (isControlledDetails) return;
    onDetailsKeyChange?.(detailsInner);
  }, [detailsInner, isControlledDetails, onDetailsKeyChange]);

  const selSet = useMemo(() => selectedKeys ?? new Set<string>(), [selectedKeys]);
  const allKeys = useMemo(() => data.map(rowKey), [data, rowKey]);
  const allSelected = selectable && allKeys.length > 0 && allKeys.every((k) => selSet.has(k));
  const someSelected = selectable && allKeys.some((k) => selSet.has(k));

  // Keep the refs the stable callbacks read in sync with the current render.
  // Assigning during render is safe here: these refs are only ever read from
  // event handlers (never during render), so there's no tearing.
  selSetRef.current = selSet;
  anchorIndexRef.current = anchorIndex;
  allKeysRef.current = allKeys;
  dataRef.current = data;
  focusedIndexRef.current = focusedIndex;
  detailsKeyRef.current = detailsKey;
  detailsInnerRef.current = detailsInner;
  onSelectionChangeRef.current = onSelectionChange;
  onDetailsKeyChangeRef.current = onDetailsKeyChange;
  onRowOpenRef.current = onRowOpen;
  rowKeyRef.current = rowKey;
  rowKeyBindingsRef.current = rowKeyBindings;
  isControlledDetailsRef.current = isControlledDetails;

  const toggleAll = useCallback(() => {
    const onSel = onSelectionChangeRef.current;
    if (!onSel) return;
    const keys = allKeysRef.current;
    const sel = selSetRef.current;
    const allOn = keys.length > 0 && keys.every((k) => sel.has(k));
    onSel(new Set<string>(allOn ? [] : keys));
  }, []);

  const toggleOne = useCallback((key: string) => {
    const onSel = onSelectionChangeRef.current;
    if (!onSel) return;
    const next = new Set<string>(selSetRef.current);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    onSel(next);
  }, []);

  // Shift-click selects an inclusive range from the last anchor to the
  // current row, OR-ing the range into the current selection. A plain
  // click resets the anchor and toggles a single row.
  const handleCheckboxClick = useCallback(
    (key: string, index: number, shiftKey: boolean) => {
      const onSel = onSelectionChangeRef.current;
      if (!onSel) return;
      const anchor = anchorIndexRef.current;
      if (shiftKey && anchor !== null && anchor !== index) {
        const [lo, hi] = anchor < index ? [anchor, index] : [index, anchor];
        const next = new Set<string>(selSetRef.current);
        const keys = allKeysRef.current;
        for (let i = lo; i <= hi; i++) {
          const k = keys[i];
          if (k !== undefined) next.add(k);
        }
        onSel(next);
        return;
      }
      setAnchorIndex(index);
      toggleOne(key);
    },
    [toggleOne],
  );

  const handleHeaderSort = useCallback(
    (col: ColumnDef<T>) => {
      if (!serverSort || !col.sortable) return;
      const nextOrder = serverSort.sortBy === col.id && serverSort.order === "asc" ? "desc" : "asc";
      serverSort.onChange({ sortBy: col.id, order: nextOrder });
    },
    [serverSort],
  );

  // onClick / onContextMenu handlers handed to every row. Stable so they
  // don't bust the row memo; the row passes back its own index/coords.
  const handleRowClick = useCallback((index: number) => {
    // If the user just drag-selected text inside the grid, the trailing click
    // shouldn't also open the row (which navigates away and clobbers the
    // selection). A plain click collapses any prior selection on mousedown, so
    // this guard only trips at the end of a real text selection.
    const sel = typeof window !== "undefined" ? window.getSelection() : null;
    if (sel && !sel.isCollapsed && sel.toString().trim() !== "") return;
    setFocusedIndex(index);
    const row = dataRef.current[index];
    if (row) onRowOpenRef.current?.(row);
  }, []);

  const handleRowContextMenu = useCallback((index: number, x: number, y: number) => {
    setFocusedIndex(index);
    const row = dataRef.current[index];
    // Capture the highlighted text now: right-click preserves the selection, so
    // this reflects what the user wants the menu's "Copy" item to copy.
    const selection =
      typeof window !== "undefined" ? (window.getSelection()?.toString() ?? "") : "";
    if (row) setCtxMenu({ row, index, x, y, selection });
  }, []);

  const handleCheckboxToggle = useCallback(
    (key: string, index: number) => {
      // Pure keyboard toggle from the Checkbox primitive — clicks land on the
      // parent td (shift-aware).
      setAnchorIndex(index);
      toggleOne(key);
    },
    [toggleOne],
  );

  const onKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTableElement>) => {
      // Don't hijack keys the user is typing into a field that happens to
      // live inside the grid (e.g. an inline editor or the search bar).
      if (isEditable(e.target)) return;
      // Single-letter shortcuts (j/k vim aliases, e/x, and consumer bindings
      // like a/c) must NOT fire when Ctrl, Cmd, or Alt is held. Those combos
      // are reserved for browser actions (Ctrl+C copy, Ctrl+A select-all,
      // Ctrl+X cut) and for the global shortcut registry (Ctrl+K command
      // palette, Ctrl+1…5 page nav). Arrow keys and Enter are navigation keys
      // that are unambiguous regardless of modifier state.
      const hasModifier = e.ctrlKey || e.metaKey || e.altKey;
      const key = e.key.toLowerCase();
      const rows = dataRef.current;
      const focused = focusedIndexRef.current;
      const rk = rowKeyRef.current;
      // j/k vim aliases mirror ArrowDown/ArrowUp; only fire unmodified so
      // Ctrl+J / Ctrl+K are not swallowed before reaching the window listener.
      if (e.key === "ArrowDown" || (!hasModifier && key === "j")) {
        e.preventDefault();
        setFocusedIndex(Math.min(rows.length - 1, focused + 1));
      } else if (e.key === "ArrowUp" || (!hasModifier && key === "k")) {
        e.preventDefault();
        setFocusedIndex(Math.max(0, focused - 1));
      } else if (e.key === "Enter") {
        const row = rows[focused];
        if (row && onRowOpenRef.current) {
          e.preventDefault();
          onRowOpenRef.current(row);
        }
      } else if (!hasModifier && key === "e" && renderDetails) {
        const row = rows[focused];
        if (row) {
          e.preventDefault();
          openDetailsAt(rk(row), focused);
        }
      } else if (!hasModifier && key === "x" && selectable) {
        const row = rows[focused];
        if (row) {
          e.preventDefault();
          toggleOne(rk(row));
        }
      } else if (!hasModifier && rowKeyBindingsRef.current) {
        // Consumer-supplied per-row bindings (a=ack, c=comment, …). These run
        // after the reserved keys above so the table's own shortcuts win.
        const row = rows[focused];
        if (row) {
          const binding = rowKeyBindingsRef.current(row)[key];
          if (binding) {
            e.preventDefault();
            binding();
          }
        }
      }
    },
    [renderDetails, selectable, openDetailsAt, toggleOne],
  );

  useEffect(() => {
    if (focusedIndex >= data.length) setFocusedIndex(data.length - 1);
  }, [data.length, focusedIndex]);

  // Auto-close the drawer when the open row leaves the data (page change,
  // filter, or refetch that drops the row — or a deep-linked ?record= that
  // isn't on the current page). Guarded on !loading so the transient empty-data
  // render during a fetch doesn't close a valid open record.
  useEffect(() => {
    if (loading) return;
    if (activeDetailsKey == null) return;
    if (!allKeys.includes(activeDetailsKey)) {
      setDetailsKey(null);
    }
  }, [loading, activeDetailsKey, allKeys, setDetailsKey]);

  const isEmpty = !loading && data.length === 0;
  const selectedRows = useMemo(
    () => data.filter((r) => selSet.has(rowKey(r))),
    [data, rowKey, selSet],
  );

  const hasSelection = selectable && bulkActions && selectedRows.length > 0;
  const hasKeyboardHints = (keyboardHints?.length ?? 0) > 0;
  const showToolbar =
    toolbar !== undefined || toolbarHeader !== undefined || hasSelection || hasKeyboardHints;

  // A kebab column renders when the page supplies row actions OR when
  // `renderDetails` is set (the auto-appended "View details" item still needs
  // the column). Same for the quick-actions column: `renderDetails` adds a
  // built-in hover-revealed "View details" IconButton, so the column must
  // exist even when the page supplies no quickActions of its own — it's the
  // per-row visual cue that the detail drawer exists. Kept in one place so
  // header / body / totalCols agree.
  const hasKebab = rowActions !== undefined || renderDetails !== undefined;
  const hasQuickCol = quickActions !== undefined || renderDetails !== undefined;

  // Total rendered columns — kept in one place so the empty-state colspan and
  // the header stay in sync.
  const totalCols =
    columns.length + (selectable ? 1 : 0) + (hasQuickCol ? 1 : 0) + (hasKebab ? 1 : 0);

  // Keyboard-shortcut legend: only built (and only shown) when the page opts in
  // via `keyboardHints`. We prepend the table's own built-in bindings — derived
  // from the capabilities actually enabled — so the legend never advertises a
  // shortcut that does nothing on this particular table.
  const keyboardShortcuts = useMemo(() => {
    if (!keyboardHints || keyboardHints.length === 0) return [];
    const builtin: { keys: string; label: string }[] = [
      { keys: "↑ ↓ · J K", label: "Move between rows" },
    ];
    if (onRowOpen) builtin.push({ keys: "Enter", label: "Open row" });
    if (renderDetails) builtin.push({ keys: "E", label: "View details" });
    if (selectable) builtin.push({ keys: "X", label: "Select / deselect row" });
    return [...builtin, ...keyboardHints];
  }, [keyboardHints, onRowOpen, renderDetails, selectable]);

  // Resolve the open row for the detail drawer. When the key maps to a visible
  // row we render the modal drawer after the table.
  const detailsIndex = activeDetailsKey != null ? allKeys.indexOf(activeDetailsKey) : -1;
  const detailsRow = detailsIndex >= 0 ? data[detailsIndex] : undefined;
  const detailsActions =
    renderDetails && detailsRow && quickActions
      ? quickActions(detailsRow).map((a) => (
          <IconButton
            key={a.key}
            icon={a.icon ?? "more-horizontal"}
            label={a.label}
            size="sm"
            {...(a.danger ? { variant: "danger" as const } : {})}
            {...(a.disabled ? { disabled: true } : {})}
            onClick={a.onSelect}
          />
        ))
      : null;

  return (
    <div className={styles.wrap}>
      {search || showToolbar ? (
        <div className={styles.toolbarRow}>
          {search ? (
            <div className={styles.searchSlot}>
              <SearchBar
                value={search.value}
                onChange={(c) => search.onChange({ text: c.text, condition: c.condition })}
                {...(search.onSubmit ? { onSubmit: search.onSubmit } : {})}
                {...(search.collection ? { collection: search.collection } : {})}
                {...(search.placeholder ? { placeholder: search.placeholder } : {})}
              />
            </div>
          ) : null}
          {showToolbar ? (
            <div
              className={hasSelection ? styles.toolbarSelected : styles.toolbar}
              role="region"
              aria-label={hasSelection ? "Bulk actions" : "Table toolbar"}
            >
              {hasSelection ? (
                <span className={styles.toolbarCount}>{selectedRows.length} selected</span>
              ) : toolbarHeader !== undefined ? (
                <span className={styles.toolbarHeader}>{toolbarHeader}</span>
              ) : null}
              <div className={styles.toolbarActions}>
                {hasSelection ? bulkActions(selectedRows) : toolbar}
                {keyboardShortcuts.length > 0 ? (
                  <Popover>
                    <PopoverTrigger asChild>
                      <IconButton
                        icon="info"
                        label="Keyboard shortcuts"
                        size="sm"
                        withTooltip={false}
                      />
                    </PopoverTrigger>
                    <PopoverContent align="end">
                      <div
                        className={styles.shortcutLegend}
                        role="list"
                        aria-label="Keyboard shortcuts"
                      >
                        {keyboardShortcuts.map((s) => (
                          <div key={s.keys} className={styles.shortcutRow} role="listitem">
                            <kbd className={styles.kbd}>{s.keys}</kbd>
                            <span>{s.label}</span>
                          </div>
                        ))}
                      </div>
                    </PopoverContent>
                  </Popover>
                ) : null}
              </div>
            </div>
          ) : null}
        </div>
      ) : null}

      <div className={styles.tableScroll}>
        <table
          ref={gridRef}
          role="grid"
          tabIndex={0}
          onKeyDown={onKeyDown}
          aria-label="Data table"
          className={`${styles.table} ${density === "compact" ? styles.dense : ""}`}
          {...(stale ? { "data-stale": "true", "aria-busy": "true" } : {})}
        >
          <thead>
            <tr className={styles.headerRow}>
              {selectable ? (
                <th className={styles.checkboxCell} scope="col">
                  <Checkbox
                    aria-label="Select all"
                    checked={allSelected ? true : someSelected ? "indeterminate" : false}
                    onCheckedChange={toggleAll}
                  />
                </th>
              ) : null}
              {columns.map((col) => (
                <th
                  key={col.id}
                  scope="col"
                  {...(col.width ? { style: { width: col.width } } : {})}
                  {...(col.hideBelow ? { "data-hide": col.hideBelow } : {})}
                  {...(col.sortable && serverSort
                    ? {
                        "aria-sort":
                          serverSort.sortBy === col.id
                            ? serverSort.order === "asc"
                              ? ("ascending" as const)
                              : ("descending" as const)
                            : ("none" as const),
                      }
                    : {})}
                >
                  {col.sortable && serverSort ? (
                    <button
                      type="button"
                      className={styles.sortBtn}
                      onClick={() => handleHeaderSort(col)}
                    >
                      {col.header}
                      {serverSort.sortBy === col.id ? (
                        <Icon
                          name={serverSort.order === "asc" ? "chevron-up" : "chevron-down"}
                          size={12}
                        />
                      ) : null}
                    </button>
                  ) : (
                    <span>{col.header}</span>
                  )}
                </th>
              ))}
              {hasQuickCol ? (
                <th className={styles.quickActionsCell} aria-label="Quick actions" />
              ) : null}
              {hasKebab ? <th className={styles.actionsCell} aria-label="Actions" /> : null}
            </tr>
          </thead>
          <tbody>
            {loading ? (
              Array.from({ length: 5 }).map((_, idx) => (
                <tr key={idx} className={styles.skeletonRow}>
                  {selectable ? (
                    <td className={styles.checkboxCell}>
                      <Skeleton width={14} height={14} />
                    </td>
                  ) : null}
                  {columns.map((c) => (
                    <td key={c.id} {...(c.hideBelow ? { "data-hide": c.hideBelow } : {})}>
                      <Skeleton height={12} />
                    </td>
                  ))}
                  {hasQuickCol ? <td className={styles.quickActionsCell} /> : null}
                  {hasKebab ? <td className={styles.actionsCell} /> : null}
                </tr>
              ))
            ) : isEmpty ? (
              <tr>
                <td colSpan={totalCols}>
                  {emptyState ?? <EmptyState icon="file-text" title="No items" />}
                </td>
              </tr>
            ) : (
              data.map((row, idx) => {
                const key = rowKey(row);
                return (
                  <DataTableRow<T>
                    key={key}
                    row={row}
                    rowKeyValue={key}
                    index={idx}
                    columns={columns}
                    selectable={selectable}
                    isSelected={selSet.has(key)}
                    isFocused={idx === focusedIndex}
                    isDisabled={rowDisabled?.(row) ?? false}
                    accent={rowAccent?.(row)}
                    hasContextMenu={contextMenuItems !== undefined || renderDetails !== undefined}
                    quickActions={quickActions}
                    rowActions={rowActions}
                    rowActionsBadge={rowActionsBadge}
                    hasDetails={renderDetails !== undefined}
                    onRowClick={handleRowClick}
                    onRowContextMenu={handleRowContextMenu}
                    onOpenDetails={openDetailsAt}
                    onCheckboxCellClick={handleCheckboxClick}
                    onCheckboxToggle={handleCheckboxToggle}
                  />
                );
              })
            )}
          </tbody>
        </table>
      </div>

      {renderDetails ? (
        <RowDetailsDrawer<T>
          rows={data}
          rowKey={rowKey}
          activeKey={activeDetailsKey ?? null}
          onNavigate={retargetDetails}
          onClose={closeDetails}
          renderDetails={renderDetails}
          detailsTitle={detailsTitle}
          actions={detailsActions}
        />
      ) : null}

      {serverPagination ? <PaginationBar pag={serverPagination} /> : null}

      {ctxMenu && (contextMenuItems || renderDetails) ? (
        <DataTableContextMenu
          items={[
            // Same auto-prepended "View details" entry as the kebab (see
            // DataTableRowInner) — kept in sync so the two affordances never
            // diverge on which row action opens the drawer.
            ...(renderDetails
              ? [
                  {
                    key: "__details__",
                    label: "View details",
                    icon: "panel-right" as const,
                    onSelect: () => openDetailsAt(rowKey(ctxMenu.row), ctxMenu.index),
                  },
                ]
              : []),
            ...(contextMenuItems ? contextMenuItems(ctxMenu.row) : []),
          ]}
          x={ctxMenu.x}
          y={ctxMenu.y}
          copyText={ctxMenu.selection}
          onClose={() => setCtxMenu(null)}
        />
      ) : null}
    </div>
  );
}

type DataTableRowProps<T> = {
  row: T;
  rowKeyValue: string;
  index: number;
  columns: ColumnDef<T>[];
  selectable: boolean;
  isSelected: boolean;
  isFocused: boolean;
  isDisabled: boolean;
  accent: string | undefined;
  hasContextMenu: boolean;
  quickActions: ((row: T) => RowAction[]) | undefined;
  rowActions: ((row: T) => RowAction[]) | undefined;
  rowActionsBadge: ((row: T) => { count: number; label?: string } | undefined) | undefined;
  /** Whether the table has a details drawer — gates the auto-appended
   *  "View details" kebab item (and the kebab column itself when no
   *  `rowActions` are supplied). */
  hasDetails: boolean;
  onRowClick: (index: number) => void;
  onRowContextMenu: (index: number, x: number, y: number) => void;
  onOpenDetails: (key: string, index: number) => void;
  onCheckboxCellClick: (key: string, index: number, shiftKey: boolean) => void;
  onCheckboxToggle: (key: string, index: number) => void;
};

// One table row, memoized with the DEFAULT shallow comparison. Function props
// (columns, quickActions, rowActions, the handlers) take part in equality, so
// the parent must keep them stable — which it does via useCallback for its
// internal handlers and which consumers do for their row-builder props. The
// payoff: a focus move or a single selection toggle changes one row's
// `isFocused`/`isSelected` prop and re-renders only that row, not all 50.
// Structural sharing from react-query keeps `row` identity stable across
// refetches, so the 5s poll skips unchanged rows too.
function DataTableRowInner<T>({
  row,
  rowKeyValue: key,
  index,
  columns,
  selectable,
  isSelected,
  isFocused,
  isDisabled,
  accent,
  hasContextMenu,
  quickActions,
  rowActions,
  rowActionsBadge,
  hasDetails,
  onRowClick,
  onRowContextMenu,
  onOpenDetails,
  onCheckboxCellClick,
  onCheckboxToggle,
}: DataTableRowProps<T>) {
  // Auto-append a "View details" item as the FIRST kebab entry when the table
  // has a details drawer. When the page supplies no `rowActions`, the kebab
  // still renders with just this item.
  const kebabActions: RowAction[] | undefined = hasDetails
    ? [
        {
          key: "__details__",
          label: "View details",
          icon: "panel-right",
          onSelect: () => onOpenDetails(key, index),
        },
        ...(rowActions ? rowActions(row) : []),
      ]
    : rowActions
      ? rowActions(row)
      : undefined;
  return (
    <tr
      className={styles.row}
      {...(isFocused ? { "data-focused": "true" } : {})}
      {...(isSelected ? { "data-selected": "true" } : {})}
      {...(isDisabled ? { "data-disabled": "true" } : {})}
      {...(accent
        ? {
            "data-accent": "true",
            style: { "--row-accent": accent } as CSSProperties,
          }
        : {})}
      onClick={() => onRowClick(index)}
      {...(hasContextMenu
        ? {
            onContextMenu: (e: React.MouseEvent<HTMLTableRowElement>) => {
              e.preventDefault();
              onRowContextMenu(index, e.clientX, e.clientY);
            },
          }
        : {})}
    >
      {selectable ? (
        <td
          className={styles.checkboxCell}
          onClick={(e) => {
            // Swallow the row-level onClick so it doesn't also open the row,
            // and route through the shift-aware selection handler.
            e.stopPropagation();
            onCheckboxCellClick(key, index, e.shiftKey);
          }}
        >
          <Checkbox
            aria-label={`Select row ${key}`}
            checked={isSelected}
            // Pointer / keyboard events on the Checkbox itself are still
            // routed to onCheckedChange; the parent td handler covers
            // shift-click on the cell area.
            onCheckedChange={() => onCheckboxToggle(key, index)}
          />
        </td>
      ) : null}
      {columns.map((col) => (
        <td
          key={col.id}
          data-label={col.header}
          {...(col.hideBelow ? { "data-hide": col.hideBelow } : {})}
          {...(col.align === "right" ? { style: { textAlign: "right" } } : {})}
        >
          {col.cell(row)}
        </td>
      ))}
      {quickActions || hasDetails ? (
        <td className={styles.quickActionsCell} onClick={(e) => e.stopPropagation()}>
          <div className={styles.quickActions}>
            {/* Built-in hover-revealed "View details" — the per-row visual cue
                that the detail drawer exists, leading the cluster so it sits in
                the same spot on every table. */}
            {hasDetails ? (
              <IconButton
                icon="panel-right"
                label="View details"
                size="sm"
                onClick={() => onOpenDetails(key, index)}
              />
            ) : null}
            {quickActions
              ? quickActions(row).map((a) => (
                  <IconButton
                    key={a.key}
                    icon={a.icon ?? "more-horizontal"}
                    label={a.label}
                    size="sm"
                    {...(a.danger ? { variant: "danger" as const } : {})}
                    {...(a.disabled ? { disabled: true } : {})}
                    onClick={a.onSelect}
                  />
                ))
              : null}
          </div>
        </td>
      ) : null}
      {kebabActions ? (
        <td className={styles.actionsCell} onClick={(e) => e.stopPropagation()}>
          <RowActionsMenu actions={kebabActions} badge={rowActionsBadge?.(row)} />
        </td>
      ) : null}
    </tr>
  );
}

// memo() erases the generic, so cast back to a generic component type. The
// default shallow comparator is intentional (see DataTableRowInner's note).
const DataTableRow = memo(DataTableRowInner) as typeof DataTableRowInner;

function PaginationBar({ pag }: { pag: NonNullable<DataTableProps<unknown>["serverPagination"]> }) {
  const totalPages = Math.max(1, Math.ceil(pag.total / pag.pageSize));
  const showing = `${(pag.page - 1) * pag.pageSize + 1}–${Math.min(pag.page * pag.pageSize, pag.total)} of ${pag.total}`;
  const atStart = pag.page <= 1;
  const atEnd = pag.page >= totalPages;
  return (
    <div className={styles.pagination}>
      <span>{showing}</span>
      <div className={styles.paginationActions}>
        <IconButton
          icon="chevrons-left"
          label="First page"
          size="sm"
          disabled={atStart}
          onClick={() => pag.onChange({ page: 1, pageSize: pag.pageSize })}
        />
        <IconButton
          icon="chevron-left"
          label="Previous page"
          size="sm"
          disabled={atStart}
          onClick={() => pag.onChange({ page: pag.page - 1, pageSize: pag.pageSize })}
        />
        <span>
          {pag.page} / {totalPages}
        </span>
        <IconButton
          icon="chevron-right"
          label="Next page"
          size="sm"
          disabled={atEnd}
          onClick={() => pag.onChange({ page: pag.page + 1, pageSize: pag.pageSize })}
        />
        <IconButton
          icon="chevrons-right"
          label="Last page"
          size="sm"
          disabled={atEnd}
          onClick={() => pag.onChange({ page: totalPages, pageSize: pag.pageSize })}
        />
      </div>
    </div>
  );
}
