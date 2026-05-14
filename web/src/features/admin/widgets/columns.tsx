import type { ColumnDef } from "@/shared/ui/DataTable";
import { Badge } from "@/shared/ui/Badge";
import { Code } from "@/shared/ui/Code";
import type { Widget } from "./types";

export const widgetColumns: ColumnDef<Widget>[] = [
  {
    id: "name",
    header: "Name",
    cell: (r) => <Code>{r.name}</Code>,
    sortable: true,
    width: "240px",
  },
  {
    id: "widget_type",
    header: "Type",
    cell: (r) => <Badge variant="neutral">{r.widget_type ?? "—"}</Badge>,
    sortable: true,
    width: "140px",
  },
  {
    id: "comment",
    header: "Comment",
    cell: (r) => <span style={{ color: "var(--text-muted)" }}>{r.comment ?? "—"}</span>,
  },
];
