import { useState } from "react";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { Card } from "@/shared/ui/Card";
import { DataTable, type ColumnDef } from "@/shared/ui/DataTable";
import type { BadgeVariant } from "@/shared/ui/Badge";
import { defineResource } from "@/lib/api/resource";

type Rule = {
  id: string;
  name: string;
  enabled: boolean;
  severity: "critical" | "error" | "warning" | "info";
};

const Rules = defineResource<Rule>("rule");

const SEVERITY_TO_VARIANT: Record<Rule["severity"], BadgeVariant> = {
  critical: "critical",
  error: "error",
  warning: "warning",
  info: "info",
};

const columns: ColumnDef<Rule>[] = [
  { id: "name", header: "Name", cell: (r) => r.name, sortable: true },
  {
    id: "enabled",
    header: "Enabled",
    cell: (r) => <Badge variant={r.enabled ? "ok" : "muted"}>{r.enabled ? "yes" : "no"}</Badge>,
  },
  {
    id: "severity",
    header: "Severity",
    cell: (r) => <Badge variant={SEVERITY_TO_VARIANT[r.severity]}>{r.severity}</Badge>,
  },
];

export function ResourcePage() {
  const [sortBy, setSortBy] = useState<string>("name");
  const [order, setOrder] = useState<"asc" | "desc">("asc");
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<Set<string>>(new Set());

  const pageSize = 10;
  const list = Rules.useList({
    offset: (page - 1) * pageSize,
    limit: pageSize,
    orderby: sortBy,
    asc: order === "asc",
  });

  return (
    <div
      style={{
        padding: "var(--space-5)",
        display: "flex",
        flexDirection: "column",
        gap: "var(--space-4)",
      }}
    >
      <h1 style={{ margin: 0 }}>Resource factory demo</h1>
      <p style={{ color: "var(--text-muted)", margin: 0 }}>
        defineResource + DataTable wired together. The endpoint is mocked by MSW only in tests; in
        dev this calls the real backend at GET /api/v1/rule.
      </p>
      <Card>
        <DataTable
          data={list.data?.data ?? []}
          columns={columns}
          rowKey={(r) => r.id}
          loading={list.isPending}
          selectable
          selectedKeys={selected}
          onSelectionChange={setSelected}
          serverSort={{
            sortBy,
            order,
            onChange: (next) => {
              setSortBy(next.sortBy);
              setOrder(next.order);
            },
          }}
          serverPagination={{
            page,
            pageSize,
            total: list.data?.meta.total ?? 0,
            onChange: (next) => setPage(next.page),
          }}
          bulkActions={(rows) => (
            <>
              <Button variant="secondary" size="sm">
                Export ({rows.length})
              </Button>
              <Button variant="danger" size="sm" leadingIcon="trash">
                Delete ({rows.length})
              </Button>
            </>
          )}
          rowActions={(r) => [
            { key: "edit", label: `Edit ${r.name}`, icon: "edit", onSelect: () => undefined },
            { key: "duplicate", label: "Duplicate", icon: "copy", onSelect: () => undefined },
            {
              key: "delete",
              label: "Delete",
              icon: "trash",
              danger: true,
              onSelect: () => undefined,
            },
          ]}
          onRowOpen={(r) => alert(`Open ${r.name}`)}
        />
      </Card>
    </div>
  );
}
