import type { ColumnDef } from "@/shared/ui/DataTable";
import { Badge } from "@/shared/ui/Badge";
import { Code } from "@/shared/ui/Code";
import type { Action, Notification } from "./types";

export const notificationColumns: ColumnDef<Notification>[] = [
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
    id: "actions",
    header: "Actions",
    cell: (r) => (
      <span style={{ display: "inline-flex", gap: "var(--space-1)", flexWrap: "wrap" }}>
        {(r.actions ?? []).map((a) => (
          <Badge key={a} variant="info">
            {a}
          </Badge>
        ))}
        {(r.actions ?? []).length === 0 ? (
          <span style={{ color: "var(--text-muted)" }}>—</span>
        ) : null}
      </span>
    ),
  },
  {
    id: "comment",
    header: "Comment",
    cell: (r) => <span style={{ color: "var(--text-muted)" }}>{r.comment ?? "—"}</span>,
  },
];

export const actionColumns: ColumnDef<Action>[] = [
  {
    id: "name",
    header: "Name",
    cell: (r) => <Code>{r.name}</Code>,
    sortable: true,
    width: "240px",
  },
  {
    id: "action_type",
    header: "Type",
    cell: (r) => <Badge variant="neutral">{r.action_type ?? "—"}</Badge>,
    sortable: true,
    width: "140px",
  },
  {
    id: "comment",
    header: "Comment",
    cell: (r) => <span style={{ color: "var(--text-muted)" }}>{r.comment ?? "—"}</span>,
  },
];
