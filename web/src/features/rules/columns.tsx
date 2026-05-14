import type { ColumnDef } from "@/shared/ui/DataTable";
import { Badge } from "@/shared/ui/Badge";
import { Code } from "@/shared/ui/Code";
import type { Rule } from "./types";

export const ruleColumns: ColumnDef<Rule>[] = [
  {
    id: "name",
    header: "Name",
    cell: (r) => <Code>{r.name}</Code>,
    sortable: true,
    width: "240px",
  },
  {
    id: "enabled",
    header: "Enabled",
    cell: (r) => (
      <Badge variant={r.enabled !== false ? "ok" : "muted"}>
        {r.enabled !== false ? "yes" : "no"}
      </Badge>
    ),
    width: "100px",
  },
  {
    id: "comment",
    header: "Comment",
    cell: (r) => <span style={{ color: "var(--text-muted)" }}>{r.comment ?? "—"}</span>,
  },
];
