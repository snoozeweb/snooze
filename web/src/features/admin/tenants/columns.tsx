import type { ColumnDef } from "@/shared/ui/DataTable";
import { Badge, type BadgeVariant } from "@/shared/ui/Badge";
import { Code } from "@/shared/ui/Code";
import type { Tenant } from "./types";

function statusVariant(status: string): BadgeVariant {
  if (status === "active") return "ok";
  if (status === "suspended") return "warning";
  return "neutral";
}

export const tenantColumns: ColumnDef<Tenant>[] = [
  {
    id: "id",
    header: "Slug",
    cell: (r) => <Code>{r.id}</Code>,
    sortable: true,
    width: "180px",
  },
  {
    id: "display_name",
    header: "Display Name",
    cell: (r) => <span>{r.display_name}</span>,
    sortable: true,
  },
  {
    id: "status",
    header: "Status",
    cell: (r) => <Badge variant={statusVariant(r.status)}>{r.status}</Badge>,
    width: "120px",
  },
];
