import { useCallback, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { Checkbox } from "./Checkbox";
import { EmptyState } from "./EmptyState";
import { Icon } from "@/shared/icons/Icon";
import type { IconName } from "@/shared/icons/icon-names";
import { IconButton } from "./IconButton";
import { Menu, MenuContent, MenuItem, MenuTrigger } from "./Menu";
import { SearchBar, type ParsedCondition } from "./SearchBar";
import { Skeleton } from "./Skeleton";
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
    /** Field-catalog collection (rule, snooze, user, …) for autocomplete. */
    collection?: string;
    placeholder?: string;
  };
  emptyState?: ReactNode;
  loading?: boolean;
  onRowOpen?: (row: T) => void;
  /** When true for a row, the row renders with muted styling — used to
   *  indicate `enabled:false` records without dedicating a column. */
  rowDisabled?: (row: T) => boolean;
  /** When provided, each row gets a chevron in a dedicated first column
   *  that toggles an inline "details" panel rendered beneath the row.
   *  Multiple rows may be expanded at once. */
  renderExpanded?: (row: T) => ReactNode;
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
  contextMenuItems,
  bulkActions,
  toolbar,
  toolbarHeader,
  search,
  emptyState,
  loading = false,
  onRowOpen,
  rowDisabled,
  renderExpanded,
}: DataTableProps<T>) {
  const [focusedIndex, setFocusedIndex] = useState<number>(-1);
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set<string>());
  const [ctxMenu, setCtxMenu] = useState<{ row: T; x: number; y: number } | null>(null);
  // Anchor index for shift-click range selection. Set on every plain click
  // of a row's checkbox; consumed when the next click arrives with shift.
  const [anchorIndex, setAnchorIndex] = useState<number | null>(null);

  const toggleExpanded = useCallback((key: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }, []);

  const selSet = useMemo(() => selectedKeys ?? new Set<string>(), [selectedKeys]);
  const allKeys = useMemo(() => data.map(rowKey), [data, rowKey]);
  const allSelected = selectable && allKeys.length > 0 && allKeys.every((k) => selSet.has(k));
  const someSelected = selectable && allKeys.some((k) => selSet.has(k));

  const toggleAll = useCallback(() => {
    if (!onSelectionChange) return;
    const next = new Set<string>(allSelected ? [] : allKeys);
    onSelectionChange(next);
  }, [allSelected, allKeys, onSelectionChange]);

  const toggleOne = useCallback(
    (key: string) => {
      if (!onSelectionChange) return;
      const next = new Set<string>(selSet);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      onSelectionChange(next);
    },
    [onSelectionChange, selSet],
  );

  // Shift-click selects an inclusive range from the last anchor to the
  // current row, OR-ing the range into the current selection. A plain
  // click resets the anchor and toggles a single row.
  const handleCheckboxClick = useCallback(
    (key: string, index: number, shiftKey: boolean) => {
      if (!onSelectionChange) return;
      if (shiftKey && anchorIndex !== null && anchorIndex !== index) {
        const [lo, hi] =
          anchorIndex < index ? [anchorIndex, index] : [index, anchorIndex];
        const next = new Set<string>(selSet);
        for (let i = lo; i <= hi; i++) {
          const k = allKeys[i];
          if (k !== undefined) next.add(k);
        }
        onSelectionChange(next);
        return;
      }
      setAnchorIndex(index);
      toggleOne(key);
    },
    [allKeys, anchorIndex, onSelectionChange, selSet, toggleOne],
  );

  const handleHeaderSort = useCallback(
    (col: ColumnDef<T>) => {
      if (!serverSort || !col.sortable) return;
      const nextOrder = serverSort.sortBy === col.id && serverSort.order === "asc" ? "desc" : "asc";
      serverSort.onChange({ sortBy: col.id, order: nextOrder });
    },
    [serverSort],
  );

  const onKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTableElement>) => {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setFocusedIndex((i) => Math.min(data.length - 1, i + 1));
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setFocusedIndex((i) => Math.max(0, i - 1));
      } else if (e.key === "Enter") {
        const row = data[focusedIndex];
        if (row && onRowOpen) {
          e.preventDefault();
          onRowOpen(row);
        }
      } else if (e.key.toLowerCase() === "x" && selectable) {
        const row = data[focusedIndex];
        if (row) {
          e.preventDefault();
          toggleOne(rowKey(row));
        }
      }
    },
    [data, focusedIndex, onRowOpen, rowKey, selectable, toggleOne],
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

  return (
    <div className={styles.wrap}>
      {search || showToolbar ? (
        <div className={styles.toolbarRow}>
          {search ? (
            <div className={styles.searchSlot}>
              <SearchBar
                value={search.value}
                onChange={(c) =>
                  search.onChange({ text: c.text, condition: c.condition })
                }
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
                  {rowActions ? <td className={styles.actionsCell} /> : null}
                </tr>
              ))
            ) : isEmpty ? (
              <tr>
                <td
                  colSpan={
                    columns.length +
                    (selectable ? 1 : 0) +
                    (rowActions ? 1 : 0) +
                    (renderExpanded ? 1 : 0)
                  }
                >
                  {emptyState ?? <EmptyState icon="file-text" title="No items" />}
                </td>
              </tr>
            ) : (
              data.flatMap((row, idx) => {
                const key = rowKey(row);
                const isSelected = selSet.has(key);
                const isFocused = idx === focusedIndex;
                const isExpanded = expanded.has(key);
                const totalCols =
                  columns.length +
                  (selectable ? 1 : 0) +
                  (rowActions ? 1 : 0) +
                  (renderExpanded ? 1 : 0);
                const rows: ReactNode[] = [
                  <tr
                    key={key}
                    className={styles.row}
                    {...(isFocused ? { "data-focused": "true" } : {})}
                    {...(isSelected ? { "data-selected": "true" } : {})}
                    {...(rowDisabled?.(row) ? { "data-disabled": "true" } : {})}
                    onClick={() => {
                      setFocusedIndex(idx);
                      onRowOpen?.(row);
                    }}
                    {...(contextMenuItems
                      ? {
                          onContextMenu: (e: React.MouseEvent<HTMLTableRowElement>) => {
                            e.preventDefault();
                            setFocusedIndex(idx);
                            setCtxMenu({ row, x: e.clientX, y: e.clientY });
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
                          onClick={() => toggleExpanded(key)}
                        >
                          <Icon
                            name={isExpanded ? "chevron-down" : "chevron-right"}
                            size={14}
                          />
                        </button>
                      </td>
                    ) : null}
                    {selectable ? (
                      <td
                        className={styles.checkboxCell}
                        onClick={(e) => {
                          // Swallow the row-level onClick so it doesn't also
                          // open the row, and route through the shift-aware
                          // selection handler.
                          e.stopPropagation();
                          handleCheckboxClick(key, idx, e.shiftKey);
                        }}
                      >
                        <Checkbox
                          aria-label={`Select row ${key}`}
                          checked={isSelected}
                          // Pointer / keyboard events on the Checkbox itself
                          // are still routed to onCheckedChange; the parent
                          // td handler covers shift-click on the cell area.
                          onCheckedChange={() => {
                            // Pure keyboard toggle from the Checkbox primitive
                            // — clicks land on the parent td (shift-aware).
                            setAnchorIndex(idx);
                            toggleOne(key);
                          }}
                        />
                      </td>
                    ) : null}
                    {columns.map((col) => (
                      <td
                        key={col.id}
                        {...(col.align === "right" ? { style: { textAlign: "right" } } : {})}
                      >
                        {col.cell(row)}
                      </td>
                    ))}
                    {rowActions ? (
                      <td className={styles.actionsCell} onClick={(e) => e.stopPropagation()}>
                        <RowActionsMenu actions={rowActions(row)} />
                      </td>
                    ) : null}
                  </tr>,
                ];
                if (renderExpanded && isExpanded) {
                  rows.push(
                    <tr key={`${key}__expanded`} className={styles.expandedRow}>
                      <td colSpan={totalCols} className={styles.expandedCell}>
                        <div className={styles.expandedPanel}>{renderExpanded(row)}</div>
                      </td>
                    </tr>,
                  );
                }
                return rows;
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
          onClose={() => setCtxMenu(null)}
        />
      ) : null}
    </div>
  );
}

function RowActionsMenu({ actions }: { actions: RowAction[] }) {
  return (
    <Menu>
      <MenuTrigger>
        <IconButton icon="more-horizontal" label="Row actions" size="sm" />
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
}

function PaginationBar({ pag }: { pag: NonNullable<DataTableProps<unknown>["serverPagination"]> }) {
  const totalPages = Math.max(1, Math.ceil(pag.total / pag.pageSize));
  const showing = `${(pag.page - 1) * pag.pageSize + 1}–${Math.min(pag.page * pag.pageSize, pag.total)} of ${pag.total}`;
  return (
    <div className={styles.pagination}>
      <span>{showing}</span>
      <div className={styles.paginationActions}>
        <IconButton
          icon="chevron-left"
          label="Previous page"
          size="sm"
          disabled={pag.page <= 1}
          onClick={() => pag.onChange({ page: pag.page - 1, pageSize: pag.pageSize })}
        />
        <span>
          {pag.page} / {totalPages}
        </span>
        <IconButton
          icon="chevron-right"
          label="Next page"
          size="sm"
          disabled={pag.page >= totalPages}
          onClick={() => pag.onChange({ page: pag.page + 1, pageSize: pag.pageSize })}
        />
      </div>
    </div>
  );
}
