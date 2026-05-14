import { useCallback, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { Checkbox } from "./Checkbox";
import { EmptyState } from "./EmptyState";
import { Icon } from "@/shared/icons/Icon";
import type { IconName } from "@/shared/icons/icon-names";
import { IconButton } from "./IconButton";
import { Menu, MenuContent, MenuItem, MenuTrigger } from "./Menu";
import { Skeleton } from "./Skeleton";
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
  bulkActions?: (rows: T[]) => ReactNode;
  emptyState?: ReactNode;
  loading?: boolean;
  onRowOpen?: (row: T) => void;
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
  bulkActions,
  emptyState,
  loading = false,
  onRowOpen,
}: DataTableProps<T>) {
  const [focusedIndex, setFocusedIndex] = useState<number>(-1);

  const selSet = useMemo(
    () => selectedKeys ?? new Set<string>(),
    [selectedKeys],
  );
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

  const handleHeaderSort = useCallback(
    (col: ColumnDef<T>) => {
      if (!serverSort || !col.sortable) return;
      const nextOrder =
        serverSort.sortBy === col.id && serverSort.order === "asc" ? "desc" : "asc";
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

  return (
    <div className={styles.wrap}>
      {selectable && bulkActions && selectedRows.length > 0 ? (
        <div className={styles.toolbar} role="region" aria-label="Bulk actions">
          <span className={styles.toolbarCount}>{selectedRows.length} selected</span>
          <div className={styles.toolbarActions}>{bulkActions(selectedRows)}</div>
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
                    columns.length + (selectable ? 1 : 0) + (rowActions ? 1 : 0)
                  }
                >
                  {emptyState ?? <EmptyState icon="file-text" title="No items" />}
                </td>
              </tr>
            ) : (
              data.map((row, idx) => {
                const key = rowKey(row);
                const isSelected = selSet.has(key);
                const isFocused = idx === focusedIndex;
                return (
                  <tr
                    key={key}
                    className={styles.row}
                    {...(isFocused ? { "data-focused": "true" } : {})}
                    {...(isSelected ? { "data-selected": "true" } : {})}
                    onClick={() => {
                      setFocusedIndex(idx);
                      onRowOpen?.(row);
                    }}
                  >
                    {selectable ? (
                      <td
                        className={styles.checkboxCell}
                        onClick={(e) => e.stopPropagation()}
                      >
                        <Checkbox
                          aria-label={`Select row ${key}`}
                          checked={isSelected}
                          onCheckedChange={() => toggleOne(key)}
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
                      <td
                        className={styles.actionsCell}
                        onClick={(e) => e.stopPropagation()}
                      >
                        <RowActionsMenu actions={rowActions(row)} />
                      </td>
                    ) : null}
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>

      {serverPagination ? <PaginationBar pag={serverPagination} /> : null}
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

function PaginationBar({
  pag,
}: {
  pag: NonNullable<DataTableProps<unknown>["serverPagination"]>;
}) {
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
          onClick={() =>
            pag.onChange({ page: pag.page - 1, pageSize: pag.pageSize })
          }
        />
        <span>
          {pag.page} / {totalPages}
        </span>
        <IconButton
          icon="chevron-right"
          label="Next page"
          size="sm"
          disabled={pag.page >= totalPages}
          onClick={() =>
            pag.onChange({ page: pag.page + 1, pageSize: pag.pageSize })
          }
        />
      </div>
    </div>
  );
}
