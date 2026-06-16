import { useMemo } from "react";
import { useSearch } from "@tanstack/react-router";
import { DataTable } from "@/shared/ui/DataTable";
import { EmptyState } from "@/shared/ui/EmptyState";
import { RowDetailPanel } from "@/shared/ui/RowDetailPanel";
import { useTableSearch } from "@/shared/hooks/useTableSearch";
import { useResourceListPage, type BaseListSearch } from "@/shared/hooks/useResourceListPage";
import { ConfirmDeleteDialog } from "@/shared/ui/resourceContextMenu";
import { ApiKeys } from "./api";
import { ApiKeyAdminEditor } from "./ApiKeyAdminEditor";
import { makeApiKeyColumns } from "./columns";
import type { ApiKey } from "./types";

// Tenant-scoped admin surface over the `apikey` resource (gated ro_apikey /
// rw_apikey). Clone of RolesPage minus the "New" button: admins inspect,
// revoke, and edit name/expiry, but never mint (keys are minted via the
// self-service /user/me/apikeys routes where subset-of-caller is enforced).
type ApiKeysSearch = BaseListSearch;

const PAGE_SIZE = 50;

// Matches the `.page` layout shared by the admin list pages (padding, flex
// column, gap). Inlined because this feature ships no .module.css.
const PAGE_STYLE: React.CSSProperties = {
  padding: "var(--space-4)",
  display: "flex",
  flexDirection: "column",
  gap: "var(--space-3)",
};

export function ApiKeysPage() {
  // useSearch with strict:false returns the validated search params; cast for local type.
  const search = useSearch({ strict: false }) as unknown as ApiKeysSearch;

  const page = search.page ?? 1;
  const orderby = search.orderby ?? "owner";
  const asc = search.asc ?? true;
  const detailUid = search.uid;

  const remove = ApiKeys.useRemove();
  const {
    updateSearch,
    selectedKeys,
    setSelectedKeys,
    confirmDelete,
    contextMenuItems,
    bulkActions,
  } = useResourceListPage<ApiKey, ApiKeysSearch>({
    to: "/web/admin/apikeys",
    remove,
    noun: "API key",
  });

  const columns = useMemo(() => makeApiKeyColumns(), []);

  const keysSearch = useTableSearch({
    collection: "apikey",
    placeholder: "owner = … AND name = …",
    onFilterChange: () => {
      if (page !== 1) updateSearch({ page: 1 });
    },
  });

  const list = ApiKeys.useList({
    offset: (page - 1) * PAGE_SIZE,
    limit: PAGE_SIZE,
    orderby,
    asc,
    ...(keysSearch.q ? { q: keysSearch.q } : {}),
  });

  return (
    <div style={PAGE_STYLE}>
      <DataTable<ApiKey>
        data={list.data?.data ?? []}
        columns={columns}
        rowKey={(r) => r.uid ?? r.name}
        loading={list.isPending}
        contextMenuItems={contextMenuItems}
        selectable
        selectedKeys={selectedKeys}
        onSelectionChange={setSelectedKeys}
        bulkActions={bulkActions}
        toolbarHeader={`${list.data?.meta.total ?? 0} API keys`}
        search={keysSearch.searchProp}
        emptyState={
          <EmptyState
            icon="lock"
            title="No API keys yet"
            description="Users mint their own API keys from their profile; they appear here for admin review."
          />
        }
        renderExpanded={(row) => (
          <RowDetailPanel
            row={row as unknown as Record<string, unknown>}
            objectType="apikey"
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
      {detailUid !== undefined ? (
        <ApiKeyAdminEditor uid={detailUid} onClose={() => updateSearch({ uid: undefined })} />
      ) : null}
      <ConfirmDeleteDialog
        state={confirmDelete.state}
        onCancel={confirmDelete.cancel}
        onConfirm={() => void confirmDelete.confirm()}
      />
    </div>
  );
}
