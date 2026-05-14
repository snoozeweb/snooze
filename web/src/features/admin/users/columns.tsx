import type { ColumnDef } from "@/shared/ui/DataTable";
import { Badge } from "@/shared/ui/Badge";
import { Code } from "@/shared/ui/Code";
import type { User } from "./types";

export const userColumns: ColumnDef<User>[] = [
  {
    id: "name",
    header: "Name",
    cell: (r) => <Code>{r.name}</Code>,
    sortable: true,
    width: "200px",
  },
  {
    id: "type",
    header: "Type",
    cell: (r) => <Badge variant="neutral">{r.type ?? "local"}</Badge>,
    width: "100px",
  },
  { id: "roles", header: "Roles", cell: (r) => (r.roles ?? []).join(", ") || "—" },
  {
    id: "comment",
    header: "Comment",
    cell: (r) => <span style={{ color: "var(--text-muted)" }}>{r.comment ?? "—"}</span>,
  },
];
