import { createElement, useCallback, useState, type ReactNode } from "react";
import { useNavigate } from "@tanstack/react-router";
import { Button } from "@/shared/ui/Button";
import type { ContextMenuItem } from "@/shared/ui/DataTableContextMenu";
import { buildResourceContextMenu, useConfirmDelete } from "@/shared/ui/resourceContextMenu";

/** Every list resource keys its rows on an optional `uid`. */
type WithUid = { uid?: string };

/**
 * Common search-param shape every list page threads through the URL. Pages
 * extend this with their own keys (tabs, dict, …) — the hook is generic over
 * the concrete `S` so those extra keys round-trip through `updateSearch`.
 */
export type BaseListSearch = {
  uid?: string | undefined;
  page?: number;
  orderby?: string;
  asc?: boolean;
};

// TanStack Router's navigate types are locked to the registered route tree at
// build time. Casting through unknown avoids type errors when the route is
// locally constructed in tests and still works when fully registered.
type NavigateFn<S> = (opts: { to: string; search: (prev: S | undefined) => S }) => Promise<void>;

export type UseResourceListPageOptions<T extends WithUid> = {
  /** Route path passed to navigate, e.g. "/web/admin/users". */
  to: string;
  /** Resource hook returning the live useRemove mutation (delete). */
  remove: { mutateAsync: (uid: string) => Promise<unknown> };
  /** Singular item noun for delete labels/toasts, e.g. "user". */
  noun: string;
  /**
   * Extra context-menu items injected between the copy actions and Delete
   * (e.g. snooze's retro-apply). Must be identity-stable across renders.
   */
  contextMenuExtras?: (row: T) => ContextMenuItem[];
};

export type ResourceListPageApi<T extends WithUid, S extends BaseListSearch> = {
  /**
   * Merge `next` into the URL search params, dropping any key explicitly set
   * to undefined (exactOptionalPropertyTypes idiom — closing a panel, leaving
   * the All tab, …). Identity-stable.
   */
  updateSearch: (next: Partial<S>) => void;
  /** Current selected-row keys. */
  selectedKeys: Set<string>;
  /** Setter handed straight to DataTable's onSelectionChange. */
  setSelectedKeys: (next: Set<string>) => void;
  /** Confirm-delete machine (state + request/cancel/confirm). */
  confirmDelete: ReturnType<typeof useConfirmDelete<T>>;
  /**
   * Context-menu builder for a row — wired to open (uid → updateSearch),
   * delete, and requestDelete. Identity-stable; safe as a DataTable prop.
   */
  contextMenuItems: (row: T) => ContextMenuItem[];
  /**
   * Default bulk-action: a single danger "Delete (N)" button that opens the
   * confirm dialog. Identity-stable. Pages with extra bulk actions (snoozes)
   * build their own and ignore this.
   */
  bulkActions: (rows: T[]) => ReactNode;
  /** Open the row's editor by writing its uid to the URL. Identity-stable. */
  openRow: (uid: string) => void;
};

/**
 * useResourceListPage — extracts the ~150 lines of identical scaffolding every
 * list page repeats: the `updateSearch` navigate callback (with the
 * undefined-stripping idiom), selection state, the useConfirmDelete +
 * buildResourceContextMenu wiring, and a default bulk-delete button.
 *
 * Deliberately a hook, not a wrapper component: each page keeps rendering its
 * own <DataTable> so columns, empty states, tabs, and toolbars stay explicit
 * and per-page. Everything returned that flows into DataTable props is
 * useCallback/useMemo'd so the DataTableRow memo is never defeated.
 *
 * Pages derive their `serverSort` / `serverPagination` / `toolbar` inline from
 * the same URL search params — those are page-shaped (different sort defaults,
 * tab-aware counts) and cheap, so they stay in the page.
 */
export function useResourceListPage<T extends WithUid, S extends BaseListSearch = BaseListSearch>(
  opts: UseResourceListPageOptions<T>,
): ResourceListPageApi<T, S> {
  const { to, remove, noun, contextMenuExtras } = opts;
  const navigate = useNavigate();

  const updateSearch = useCallback(
    (next: Partial<S>) => {
      void (navigate as unknown as NavigateFn<S>)({
        to,
        search: (prev: S | undefined) => {
          const merged: Record<string, unknown> = { ...(prev ?? {}), ...next };
          // exactOptionalPropertyTypes: drop keys explicitly set to undefined
          // rather than carrying them through (closing the detail panel,
          // returning to the "All" tab, …).
          for (const key of Object.keys(merged)) {
            if (merged[key] === undefined) delete merged[key];
          }
          return merged as S;
        },
      });
    },
    [navigate, to],
  );

  const openRow = useCallback((uid: string) => updateSearch({ uid } as Partial<S>), [updateSearch]);

  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(new Set());
  const clearSelection = useCallback(() => setSelectedKeys(new Set()), []);

  // useMutation's result object changes identity on status flips, but its
  // mutateAsync method is referentially stable — capture it so the callbacks
  // below don't re-create on every mutation state change.
  const deleteOne = remove.mutateAsync;
  const confirmDelete = useConfirmDelete<T>({
    onDelete: deleteOne,
    noun,
    onAfter: clearSelection,
  });
  // useConfirmDelete returns a fresh object literal each render, but its
  // `request` action is stable. Depending on the action (not the wrapper)
  // keeps the row-memo-critical callbacks below identity-stable.
  const requestDelete = confirmDelete.request;

  const contextMenuItems = useCallback(
    (row: T): ContextMenuItem[] =>
      buildResourceContextMenu(row, {
        onDelete: (uid) => deleteOne(uid),
        requestDelete: (r) => requestDelete([r]),
        ...(contextMenuExtras ? { extras: contextMenuExtras } : {}),
      }),
    [deleteOne, requestDelete, contextMenuExtras],
  );

  const bulkActions = useCallback(
    (rows: T[]) =>
      createElement(
        Button,
        {
          size: "sm",
          variant: "danger",
          leadingIcon: "trash",
          onClick: () => requestDelete(rows),
        },
        `Delete (${rows.length})`,
      ),
    [requestDelete],
  );

  // No outer memo: `confirmDelete` is a fresh object each render (it carries
  // the live dialog `state`), so memoizing the wrapper would never pay off.
  // The row-memo-critical fields (contextMenuItems, bulkActions, updateSearch,
  // setSelectedKeys, openRow) are each individually stable, which is what the
  // DataTableRow memo actually depends on.
  return {
    updateSearch,
    selectedKeys,
    setSelectedKeys,
    confirmDelete,
    contextMenuItems,
    bulkActions,
    openRow,
  };
}
