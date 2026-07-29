import { useMemo } from "react";
import { useSearch, useNavigate, useLocation } from "@tanstack/react-router";
import { Badge } from "@/shared/ui/Badge";
import { DataTable, type ColumnDef } from "@/shared/ui/DataTable";
import { EmptyState } from "@/shared/ui/EmptyState";
import { JsonViewer } from "@/shared/ui/JsonViewer";
import { TimeCell } from "@/shared/ui/TimeCell";
import { useTableSearch } from "@/shared/hooks/useTableSearch";
import { ACTION_LABEL, ACTION_VARIANT } from "@/features/audit/maps";
import type { AuditEntry } from "@/features/audit/types";
import type { Condition } from "@/lib/condition/types";
import { useAuthAudit } from "./api";
import styles from "./AuthAuditPage.module.css";

const PAGE_SIZE = 50;

type AuthAuditSearch = {
  page?: number;
  orderby?: string;
  asc?: boolean;
  search?: string;
};

const COLUMNS: ColumnDef<AuditEntry>[] = [
  {
    id: "date_epoch",
    header: "Timestamp",
    sortable: true,
    cell: (row) => <TimeCell epoch={row.date_epoch} />,
    width: "12rem",
  },
  {
    id: "action",
    header: "Action",
    sortable: true,
    cell: (row) => <Badge variant={ACTION_VARIANT[row.action]}>{ACTION_LABEL[row.action]}</Badge>,
    width: "9rem",
  },
  {
    id: "username",
    header: "Username",
    sortable: true,
    cell: (row) => row.username || "—",
  },
  {
    id: "method",
    header: "Method",
    sortable: true,
    cell: (row) => row.method ?? "—",
    width: "7rem",
    hideBelow: "lg",
  },
  {
    id: "summary",
    header: "Summary",
    cell: (row) => row.summary ?? "",
  },
];

export function AuthAuditPage() {
  const search = useSearch({ strict: false }) as unknown as AuthAuditSearch;
  const navigate = useNavigate();
  const { pathname } = useLocation();

  const page = search.page ?? 1;
  const orderby = search.orderby;
  const asc = search.asc;

  type NavigateFn = (opts: {
    to: string;
    search: (prev: Record<string, unknown> | undefined) => Record<string, unknown>;
  }) => Promise<void>;

  function updateSearch(patch: Partial<AuthAuditSearch>) {
    void (navigate as unknown as NavigateFn)({
      to: pathname,
      search: (prev) => {
        const merged: Record<string, unknown> = { ...(prev ?? {}) };
        for (const [k, v] of Object.entries(patch)) {
          if (v === undefined) delete merged[k];
          else merged[k] = v;
        }
        return merged;
      },
    });
  }

  const tableSearch = useTableSearch({
    collection: "audit",
    placeholder: "username = … AND action = login_failed",
    onFilterChange: () => {
      if (page !== 1) updateSearch({ page: 1 });
    },
  });

  const filter = useMemo((): Condition | undefined => {
    if (
      !tableSearch.condition ||
      tableSearch.condition.op === "" ||
      tableSearch.condition.op === "ALWAYS_TRUE"
    ) {
      return undefined;
    }
    return tableSearch.condition as unknown as Condition;
  }, [tableSearch.condition]);

  const result = useAuthAudit(filter, {
    limit: PAGE_SIZE,
    offset: (page - 1) * PAGE_SIZE,
    ...(orderby !== undefined ? { orderby } : {}),
    ...(asc !== undefined ? { asc } : {}),
  });

  const rows = result.data?.data ?? [];
  const total = result.data?.meta.total ?? 0;

  return (
    <div className={styles.page}>
      <DataTable<AuditEntry>
        data={rows}
        columns={COLUMNS}
        rowKey={(r) => r.uid ?? `${r.date_epoch ?? 0}-${r.action}`}
        loading={result.isPending}
        search={tableSearch.searchProp}
        toolbarHeader={`${total} events`}
        // JsonViewer, not RowDetailPanel — these rows ARE audit-log entries, so
        // an "Audit log" section inside the drawer would be circular. Read-only
        // page: no contextMenuItems/rowActions are added, so the auto "View
        // details" kebab entry is the only per-row affordance.
        renderDetails={(row) => <JsonViewer value={row as unknown as Record<string, unknown>} />}
        detailsTitle={(row) =>
          `${ACTION_LABEL[row.action]}${row.username ? ` — ${row.username}` : ""}`
        }
        emptyState={
          <EmptyState
            icon="lock"
            title="No auth events recorded yet"
            description="Auth events (login, logout, refresh) will appear here once activity is recorded."
          />
        }
        serverSort={{
          sortBy: orderby ?? "date_epoch",
          order: asc === false ? "desc" : "asc",
          onChange: (next) =>
            updateSearch({
              orderby: next.sortBy,
              asc: next.order === "asc",
              page: 1,
            }),
        }}
        serverPagination={{
          page,
          pageSize: PAGE_SIZE,
          total,
          onChange: (next) => updateSearch({ page: next.page }),
        }}
      />
    </div>
  );
}
