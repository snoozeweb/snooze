import type { ColumnDef } from "@/shared/ui/DataTable";
import { Badge, type BadgeVariant } from "@/shared/ui/Badge";
import { Code } from "@/shared/ui/Code";
import type { TenantMatchRule } from "./types";

function matchTypeVariant(matchType: string): BadgeVariant {
  if (matchType === "group") return "info";
  if (matchType === "domain") return "warning";
  return "neutral";
}

function matchTypeLabel(matchType: string): string {
  if (matchType === "group") return "Group";
  if (matchType === "domain") return "Domain";
  if (matchType === "login") return "Login";
  return matchType;
}

export const tenantMatchColumns: ColumnDef<TenantMatchRule>[] = [
  {
    id: "match_type",
    header: "Match type",
    cell: (r) => (
      <Badge variant={matchTypeVariant(r.match_type)}>{matchTypeLabel(r.match_type)}</Badge>
    ),
    sortable: true,
    width: "130px",
  },
  {
    id: "match",
    header: "Match value",
    cell: (r) => <Code>{r.match}</Code>,
    sortable: true,
  },
  {
    id: "tenant_id",
    header: "Tenant",
    cell: (r) => <Code>{r.tenant_id}</Code>,
    sortable: true,
    width: "160px",
  },
  {
    id: "priority",
    header: "Priority",
    cell: (r) => (
      <span style={{ fontVariantNumeric: "tabular-nums", textAlign: "right", display: "block" }}>
        {r.priority ?? 0}
      </span>
    ),
    sortable: true,
    width: "90px",
  },
];
