import { useCallback, useMemo, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { Button } from "@/shared/ui/Button";
import { DataTable } from "@/shared/ui/DataTable";
import type { ContextMenuItem } from "@/shared/ui/DataTableContextMenu";
import { EmptyState } from "@/shared/ui/EmptyState";
import { RowDetailPanel } from "@/shared/ui/RowDetailPanel";
import { TabList, TabPanel, TabTrigger, Tabs } from "@/shared/ui/Tabs";
import { useTableSearch } from "@/shared/hooks/useTableSearch";
import { encodeConditionQ } from "@/lib/condition/serialize";
import type { Condition } from "@/lib/condition/types";
import {
  buildResourceContextMenu,
  ConfirmDeleteDialog,
  useConfirmDelete,
} from "@/shared/ui/resourceContextMenu";
import { KVs } from "./api";
import { KVEditor } from "./KVEditor";
import { kvColumns } from "./columns";
import type { KV } from "./types";
import styles from "./KVPage.module.css";

type KVSearch = {
  uid?: string | undefined;
  page?: number;
  orderby?: string;
  asc?: boolean;
  /** Active dictionary tab. Undefined / "" means the "All" tab. The backend
   *  rejects an empty dict, so "" is a safe sentinel that can never collide
   *  with a real dictionary name. */
  dict?: string | undefined;
};

// TanStack Router's navigate types are locked to the registered route tree at
// build time. Casting through unknown avoids type errors when the route is
// locally constructed in tests and still works when fully registered.
type NavigateFn = (opts: {
  to: string;
  search: (prev: KVSearch | undefined) => KVSearch;
}) => Promise<void>;

const PAGE_SIZE = 50;

export function KVPage() {
  // useSearch with strict:false returns the validated search params; cast for local type.
  const search = useSearch({ strict: false }) as unknown as KVSearch;
  const navigate = useNavigate();

  const page = search.page ?? 1;
  const orderby = search.orderby ?? "key";
  const asc = search.asc ?? true;
  const detailUid = search.uid;
  const activeDict = search.dict ?? "";
  const [creating, setCreating] = useState(false);

  const updateSearch = useCallback(
    (next: KVSearch) => {
      void (navigate as unknown as NavigateFn)({
        to: "/web/admin/kv",
        search: (prev: KVSearch | undefined) => {
          const merged: Record<string, unknown> = { ...(prev ?? {}), ...next };
          // exactOptionalPropertyTypes: drop keys explicitly set to undefined
          // rather than carrying them through (e.g. closing the detail panel
          // or returning to the "All" tab).
          for (const key of Object.keys(merged)) {
            if (merged[key] === undefined) delete merged[key];
          }
          return merged as KVSearch;
        },
      });
    },
    [navigate],
  );

  // Discover every dictionary so we can show one tab per dict. This is a
  // separate, unbounded list (no ?q, no pagination) because the main list is
  // paginated and dict-filtered and so can't enumerate all dictionaries. KV is
  // a small config store, so fetching every row just for the dict facet is
  // cheap; the query shares the "kv" cache key and is invalidated on any
  // create/update/delete, so a new dictionary's tab appears immediately.
  const dictsQuery = KVs.useList({ orderby: "dict", asc: true });
  const dicts = useMemo(() => {
    const set = new Set<string>();
    for (const row of dictsQuery.data?.data ?? []) {
      if (row.dict) set.add(row.dict);
    }
    return Array.from(set).sort((a, b) => a.localeCompare(b));
  }, [dictsQuery.data]);
  // Only worth a tab bar once there's more than one dictionary to switch
  // between — a single dict (or an empty store) shows the bare table.
  const showTabs = dicts.length > 1;

  const kvSearch = useTableSearch({
    collection: "kv",
    placeholder: "dict = … AND key MATCHES …",
    onFilterChange: () => {
      if (page !== 1) updateSearch({ page: 1 });
    },
  });

  // Combine the active dict tab's `dict = …` preset with the SearchBar's
  // condition into a single ?q=. The "All" tab has no preset, so a clean
  // search input collapses to no filter at all (the request stays cacheable).
  const q = useMemo(() => {
    const parts: Condition[] = [];
    if (activeDict) {
      parts.push({ type: "EQUALS", field: "dict", value: activeDict });
    }
    if (
      kvSearch.condition &&
      kvSearch.condition.op !== "" &&
      kvSearch.condition.op !== "ALWAYS_TRUE"
    ) {
      parts.push(kvSearch.condition as unknown as Condition);
    }
    if (parts.length === 0) return undefined;
    const combined: Condition =
      parts.length === 1 ? (parts[0] as Condition) : { type: "AND", args: parts };
    return encodeConditionQ(combined);
  }, [activeDict, kvSearch.condition]);

  const list = KVs.useList({
    offset: (page - 1) * PAGE_SIZE,
    limit: PAGE_SIZE,
    orderby,
    asc,
    ...(q ? { q } : {}),
  });
  const remove = KVs.useRemove();
  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(new Set());
  const confirmDelete = useConfirmDelete<KV>({
    onDelete: (uid) => remove.mutateAsync(uid),
    noun: "key-value",
    onAfter: () => setSelectedKeys(new Set()),
  });
  const contextMenuItems = useCallback(
    (row: KV): ContextMenuItem[] =>
      buildResourceContextMenu(row, {
        onOpen: (r) => {
          if (r.uid) updateSearch({ uid: r.uid });
        },
        onDelete: (uid) => remove.mutateAsync(uid),
        requestDelete: (r) => confirmDelete.request([r]),
      }),
    [updateSearch, remove, confirmDelete],
  );
  const bulkActions = useCallback(
    (rows: KV[]) => (
      <Button
        size="sm"
        variant="danger"
        leadingIcon="trash"
        onClick={() => confirmDelete.request(rows)}
      >
        Delete ({rows.length})
      </Button>
    ),
    [confirmDelete],
  );

  const table = (
    <DataTable<KV>
      data={list.data?.data ?? []}
      columns={kvColumns}
      rowKey={(r) => r.uid ?? r.key}
      loading={list.isPending}
      contextMenuItems={contextMenuItems}
      selectable
      selectedKeys={selectedKeys}
      onSelectionChange={setSelectedKeys}
      bulkActions={bulkActions}
      toolbarHeader={`${list.data?.meta.total ?? 0} key-values`}
      toolbar={
        <Button size="sm" variant="primary" leadingIcon="plus" onClick={() => setCreating(true)}>
          New
        </Button>
      }
      search={kvSearch.searchProp}
      emptyState={
        <EmptyState
          icon="file-text"
          title="No key-values yet"
          description="Configuration values modifications and plugins can read at runtime."
          action={
            <Button
              size="md"
              variant="primary"
              leadingIcon="plus"
              onClick={() => setCreating(true)}
            >
              New key-value
            </Button>
          }
        />
      }
      renderExpanded={(row) => (
        <RowDetailPanel
          row={row as unknown as Record<string, unknown>}
          objectType="kv"
          objectId={row.uid}
        />
      )}
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
      onRowOpen={(row) => {
        if (row.uid) updateSearch({ uid: row.uid });
      }}
    />
  );

  return (
    <div className={styles.page}>
      {showTabs ? (
        <Tabs
          value={activeDict}
          onValueChange={(v) => updateSearch({ dict: v === "" ? undefined : v, page: 1 })}
        >
          <TabList>
            <TabTrigger value="">All</TabTrigger>
            {dicts.map((d) => (
              <TabTrigger key={d} value={d}>
                {d}
              </TabTrigger>
            ))}
          </TabList>
          <TabPanel value={activeDict}>{table}</TabPanel>
        </Tabs>
      ) : (
        table
      )}
      {detailUid !== undefined ? (
        <KVEditor uid={detailUid} onClose={() => updateSearch({ uid: undefined })} />
      ) : null}
      {creating ? <KVEditor uid={undefined} onClose={() => setCreating(false)} /> : null}
      <ConfirmDeleteDialog
        state={confirmDelete.state}
        onCancel={confirmDelete.cancel}
        onConfirm={() => void confirmDelete.confirm()}
      />
    </div>
  );
}
