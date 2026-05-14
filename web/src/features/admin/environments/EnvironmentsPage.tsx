import { useCallback, useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { Button } from "@/shared/ui/Button";
import { DataTable } from "@/shared/ui/DataTable";
import { Environments } from "./api";
import { EnvironmentEditor } from "./EnvironmentEditor";
import { environmentColumns } from "./columns";
import type { Environment } from "./types";
import styles from "./EnvironmentsPage.module.css";

type EnvironmentsSearch = {
  uid?: string;
  page?: number;
  orderby?: string;
  asc?: boolean;
};

// TanStack Router's navigate types are locked to the registered route tree at
// build time. Casting through unknown avoids type errors when the route is
// locally constructed in tests and still works when fully registered.
type NavigateFn = (opts: {
  to: string;
  search: (prev: EnvironmentsSearch | undefined) => EnvironmentsSearch;
}) => Promise<void>;

const PAGE_SIZE = 50;

export function EnvironmentsPage() {
  // useSearch with strict:false returns the validated search params; cast for local type.
  const search = useSearch({ strict: false }) as unknown as EnvironmentsSearch;
  const navigate = useNavigate();

  const page = search.page ?? 1;
  const orderby = search.orderby ?? "name";
  const asc = search.asc ?? true;
  const detailUid = search.uid;
  const [creating, setCreating] = useState(false);

  const updateSearch = useCallback(
    (next: EnvironmentsSearch) => {
      void (navigate as unknown as NavigateFn)({
        to: "/web/admin/environments",
        search: (prev: EnvironmentsSearch | undefined) => {
          const merged = { ...(prev ?? {}), ...next };
          // exactOptionalPropertyTypes: remove keys set to undefined rather than keeping them
          if (merged.uid === undefined) {
            const { uid: _uid, ...rest } = merged; // eslint-disable-line @typescript-eslint/no-unused-vars
            return rest as EnvironmentsSearch;
          }
          return merged as EnvironmentsSearch;
        },
      });
    },
    [navigate],
  );

  const list = Environments.useList({
    offset: (page - 1) * PAGE_SIZE,
    limit: PAGE_SIZE,
    orderby,
    asc,
  });

  return (
    <div className={styles.page}>
      <div className={styles.topbar}>
        <span style={{ color: "var(--text-muted)", fontSize: "var(--text-sm)" }}>
          {list.data?.meta.total ?? 0} environments
        </span>
        <Button size="sm" variant="primary" leadingIcon="plus" onClick={() => setCreating(true)}>
          New
        </Button>
      </div>
      <DataTable<Environment>
        data={list.data?.data ?? []}
        columns={environmentColumns}
        rowKey={(r) => r.uid ?? r.name}
        loading={list.isPending}
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
        <EnvironmentEditor uid={detailUid} onClose={() => updateSearch({})} />
      ) : null}
      {creating ? <EnvironmentEditor uid={undefined} onClose={() => setCreating(false)} /> : null}
    </div>
  );
}
