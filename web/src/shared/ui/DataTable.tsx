import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, ReactNode } from "react";
import { Checkbox } from "./Checkbox";
import { EmptyState } from "./EmptyState";
import { Icon } from "@/shared/icons/Icon";
import type { IconName } from "@/shared/icons/icon-names";
import { IconButton } from "./IconButton";
import { Menu, MenuContent, MenuItem, MenuTrigger } from "./Menu";
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
};

export type RowAction = {
  key: string;
  label: string;
  icon?: IconName;
  danger?: boolean;
  disabled?: boolean;
  onSelect: () => void;
};

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
  /** When provided, each row gets a chevron in a dedicated first column
   *  that toggles an inline "details" panel rendered beneath the row.
   *  Multiple rows may be expanded at once. */
  renderExpanded?: (row: T) => ReactNode;
  /** Controlled expansion. When supplied, the table renders exactly this set
   *  and routes every toggle through `onExpandedChange` instead of keeping
   *  its own state. Omit it for the (unchanged) uncontrolled default. */
  expandedKeys?: ReadonlySet<string>;
  /** Fires whenever the set of expanded row keys changes. Lets the parent
   *  react to "user is actively reading a row" without pulling expansion
   *  state out of the table (e.g. pause polling on the alerts page). Also
   *  the write channel for controlled expansion (`expandedKeys`). */
  onExpandedChange?: (expandedKeys: ReadonlySet<string>) => void;
  /** Per-row keyboard shortcuts for the focused row, keyed by lowercase
   *  single key (e.g. `{ a: ackFn, c: commentFn }`). Bindings are ignored
   *  while the user is typing into an editable field, and when any modifier
   *  key (Ctrl/Cmd/Alt) is held — those combos are reserved for browser
   *  actions (Ctrl+C copy, Ctrl+A select-all) and the global shortcut
   *  registry. Reserved unmodified keys (arrows, j/k, e, x, Enter) are
   *  handled by the table itself and take precedence. */
  rowKeyBindings?: (row: T) => Record<string, () => void>;
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
  renderExpanded,
  expandedKeys,
  onExpandedChange,
  rowKeyBindings,
}: DataTableProps<T>) {
  const [focusedIndex, setFocusedIndex] = useState<number>(-1);
  // Uncontrolled expansion store. Ignored when `expandedKeys` is supplied —
  // the controlled path reads from the prop and never touches this. The
  // uncontrolled default path is therefore byte-identical in behaviour.
  const [expandedInner, setExpandedInner] = useState<Set<string>>(() => new Set<string>());
  const isControlledExpansion = expandedKeys !== undefined;
  const expanded = isControlledExpansion ? expandedKeys : expandedInner;
  const [ctxMenu, setCtxMenu] = useState<{
    row: T;
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
  const expandedKeysRef = useRef<ReadonlySet<string> | undefined>(expandedKeys);
  const onSelectionChangeRef = useRef(onSelectionChange);
  const onExpandedChangeRef = useRef(onExpandedChange);
  const onRowOpenRef = useRef(onRowOpen);
  const rowKeyRef = useRef(rowKey);
  const rowKeyBindingsRef = useRef(rowKeyBindings);
  const isControlledExpansionRef = useRef(isControlledExpansion);

  const toggleExpanded = useCallback((key: string) => {
    if (isControlledExpansionRef.current) {
      // Controlled: compute the next set off the current prop and hand it
      // back; the parent owns the state.
      const next = new Set(expandedKeysRef.current);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      onExpandedChangeRef.current?.(next);
      return;
    }
    setExpandedInner((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }, []);

  // Surface expansion changes to the parent so it can pause polling, etc.
  // Only the uncontrolled path fires from here — in the controlled path the
  // parent already owns the set and toggleExpanded calls onExpandedChange
  // directly, so re-firing on every render would double-notify.
  // The initial empty-set fire on mount is harmless — consumers treat
  // size === 0 as "nothing expanded", which matches the default state.
  useEffect(() => {
    if (isControlledExpansion) return;
    onExpandedChange?.(expandedInner);
  }, [expandedInner, isControlledExpansion, onExpandedChange]);

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
  expandedKeysRef.current = expandedKeys;
  onSelectionChangeRef.current = onSelectionChange;
  onExpandedChangeRef.current = onExpandedChange;
  onRowOpenRef.current = onRowOpen;
  rowKeyRef.current = rowKey;
  rowKeyBindingsRef.current = rowKeyBindings;
  isControlledExpansionRef.current = isControlledExpansion;

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
    if (row) setCtxMenu({ row, x, y, selection });
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
        setFocusedIndex((i) => Math.min(rows.length - 1, i + 1));
      } else if (e.key === "ArrowUp" || (!hasModifier && key === "k")) {
        e.preventDefault();
        setFocusedIndex((i) => Math.max(0, i - 1));
      } else if (e.key === "Enter") {
        const row = rows[focused];
        if (row && onRowOpenRef.current) {
          e.preventDefault();
          onRowOpenRef.current(row);
        }
      } else if (!hasModifier && key === "e" && renderExpanded) {
        const row = rows[focused];
        if (row) {
          e.preventDefault();
          toggleExpanded(rk(row));
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
    [renderExpanded, selectable, toggleExpanded, toggleOne],
  );

  useEffect(() => {
    if (focusedIndex >= data.length) setFocusedIndex(data.length - 1);
  }, [data.length, focusedIndex]);

  const isEmpty = !loading && data.length === 0;
  const selectedRows = useMemo(
    () => data.filter((r) => selSet.has(rowKey(r))),
    [data, rowKey, selSet],
  );

  const hasSelection = selectable && bulkActions && selectedRows.length > 0;
  const showToolbar = toolbar !== undefined || toolbarHeader !== undefined || hasSelection;

  // Total rendered columns — kept in one place so the empty-state colspan,
  // the expanded-panel colspan, and the header all stay in sync.
  const totalCols =
    columns.length +
    (selectable ? 1 : 0) +
    (renderExpanded ? 1 : 0) +
    (quickActions ? 1 : 0) +
    (rowActions ? 1 : 0);

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
              </div>
            </div>
          ) : null}
        </div>
      ) : null}

      <div className={styles.tableScroll}>
        <table
          role="grid"
          tabIndex={0}
          onKeyDown={onKeyDown}
          aria-label="Data table"
          className={`${styles.table} ${density === "compact" ? styles.dense : ""}`}
          {...(stale ? { "data-stale": "true", "aria-busy": "true" } : {})}
        >
          <thead>
            <tr className={styles.headerRow}>
              {renderExpanded ? <th className={styles.expandCell} aria-label="Expand" /> : null}
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
              {quickActions ? (
                <th className={styles.quickActionsCell} aria-label="Quick actions" />
              ) : null}
              {rowActions ? <th className={styles.actionsCell} aria-label="Actions" /> : null}
            </tr>
          </thead>
          <tbody>
            {loading ? (
              Array.from({ length: 5 }).map((_, idx) => (
                <tr key={idx} className={styles.skeletonRow}>
                  {renderExpanded ? <td className={styles.expandCell} /> : null}
                  {selectable ? (
                    <td className={styles.checkboxCell}>
                      <Skeleton width={14} height={14} />
                    </td>
                  ) : null}
                  {columns.map((c) => (
                    <td key={c.id}>
                      <Skeleton height={12} />
                    </td>
                  ))}
                  {quickActions ? <td className={styles.quickActionsCell} /> : null}
                  {rowActions ? <td className={styles.actionsCell} /> : null}
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
                    isExpanded={expanded.has(key)}
                    isDisabled={rowDisabled?.(row) ?? false}
                    accent={rowAccent?.(row)}
                    totalCols={totalCols}
                    hasContextMenu={contextMenuItems !== undefined}
                    quickActions={quickActions}
                    rowActions={rowActions}
                    rowActionsBadge={rowActionsBadge}
                    renderExpanded={renderExpanded}
                    onRowClick={handleRowClick}
                    onRowContextMenu={handleRowContextMenu}
                    onToggleExpanded={toggleExpanded}
                    onCheckboxCellClick={handleCheckboxClick}
                    onCheckboxToggle={handleCheckboxToggle}
                  />
                );
              })
            )}
          </tbody>
        </table>
      </div>

      {serverPagination ? <PaginationBar pag={serverPagination} /> : null}

      {ctxMenu && contextMenuItems ? (
        <DataTableContextMenu
          items={contextMenuItems(ctxMenu.row)}
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
  isExpanded: boolean;
  isDisabled: boolean;
  accent: string | undefined;
  totalCols: number;
  hasContextMenu: boolean;
  quickActions: ((row: T) => RowAction[]) | undefined;
  rowActions: ((row: T) => RowAction[]) | undefined;
  rowActionsBadge: ((row: T) => { count: number; label?: string } | undefined) | undefined;
  renderExpanded: ((row: T) => ReactNode) | undefined;
  onRowClick: (index: number) => void;
  onRowContextMenu: (index: number, x: number, y: number) => void;
  onToggleExpanded: (key: string) => void;
  onCheckboxCellClick: (key: string, index: number, shiftKey: boolean) => void;
  onCheckboxToggle: (key: string, index: number) => void;
};

// One table row, memoized with the DEFAULT shallow comparison. Function props
// (columns, quickActions, rowActions, renderExpanded, the handlers) take part
// in equality, so the parent must keep them stable — which it does via
// useCallback for its internal handlers and which consumers do for their
// row-builder props. The payoff: a focus move or a single selection toggle
// changes one row's `isFocused`/`isSelected` prop and re-renders only that
// row, not all 50. Structural sharing from react-query keeps `row` identity
// stable across refetches, so the 5s poll skips unchanged rows too.
function DataTableRowInner<T>({
  row,
  rowKeyValue: key,
  index,
  columns,
  selectable,
  isSelected,
  isFocused,
  isExpanded,
  isDisabled,
  accent,
  totalCols,
  hasContextMenu,
  quickActions,
  rowActions,
  rowActionsBadge,
  renderExpanded,
  onRowClick,
  onRowContextMenu,
  onToggleExpanded,
  onCheckboxCellClick,
  onCheckboxToggle,
}: DataTableRowProps<T>) {
  return (
    <>
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
        {renderExpanded ? (
          <td className={styles.expandCell} onClick={(e) => e.stopPropagation()}>
            <button
              type="button"
              className={styles.expandBtn}
              aria-label={`Expand row ${key}`}
              aria-expanded={isExpanded}
              onClick={() => onToggleExpanded(key)}
            >
              <Icon name={isExpanded ? "chevron-down" : "chevron-right"} size={14} />
            </button>
          </td>
        ) : null}
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
            {...(col.align === "right" ? { style: { textAlign: "right" } } : {})}
          >
            {col.cell(row)}
          </td>
        ))}
        {quickActions ? (
          <td className={styles.quickActionsCell} onClick={(e) => e.stopPropagation()}>
            <div className={styles.quickActions}>
              {quickActions(row).map((a) => (
                <IconButton
                  key={a.key}
                  icon={a.icon ?? "more-horizontal"}
                  label={a.label}
                  size="sm"
                  {...(a.danger ? { variant: "danger" as const } : {})}
                  {...(a.disabled ? { disabled: true } : {})}
                  onClick={a.onSelect}
                />
              ))}
            </div>
          </td>
        ) : null}
        {rowActions ? (
          <td className={styles.actionsCell} onClick={(e) => e.stopPropagation()}>
            <RowActionsMenu actions={rowActions(row)} badge={rowActionsBadge?.(row)} />
          </td>
        ) : null}
      </tr>
      {renderExpanded && isExpanded ? (
        <tr className={styles.expandedRow}>
          <td colSpan={totalCols} className={styles.expandedCell}>
            <div className={styles.expandedPanel}>{renderExpanded(row)}</div>
          </td>
        </tr>
      ) : null}
    </>
  );
}

// memo() erases the generic, so cast back to a generic component type. The
// default shallow comparator is intentional (see DataTableRowInner's note).
const DataTableRow = memo(DataTableRowInner) as typeof DataTableRowInner;

function RowActionsMenu({
  actions,
  badge,
}: {
  actions: RowAction[];
  badge?: { count: number; label?: string } | undefined;
}) {
  const showBadge = !!badge && badge.count > 0;
  // Fold the badge meaning into the trigger's accessible name so the pill
  // isn't a sighted-only signal. Radix MenuTrigger is asChild → the IconButton
  // must stay its direct child, so the pill is an absolutely-positioned
  // sibling (pointer-events:none) anchored by the relative wrapper.
  const triggerLabel = showBadge && badge?.label ? `Row actions, ${badge.label}` : "Row actions";
  const menu = (
    <Menu>
      <MenuTrigger>
        <IconButton icon="more-horizontal" label={triggerLabel} size="sm" />
      </MenuTrigger>
      <MenuContent>
        {actions.map((a) => (
          <MenuItem
            key={a.key}
            {...(a.icon ? { leadingIcon: a.icon } : {})}
            {...(a.danger ? { danger: true } : {})}
            {...(a.disabled ? { disabled: true } : {})}
            onSelect={a.onSelect}
          >
            {a.label}
          </MenuItem>
        ))}
      </MenuContent>
    </Menu>
  );
  if (!showBadge) return menu;
  return (
    <span className={styles.actionsBadgeWrap}>
      {menu}
      <span className={styles.actionsBadge} aria-hidden="true">
        {badge.count > 99 ? "99+" : badge.count}
      </span>
    </span>
  );
}

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
