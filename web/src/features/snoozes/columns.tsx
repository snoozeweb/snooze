import type { ColumnDef } from "@/shared/ui/DataTable";
import { Badge } from "@/shared/ui/Badge";
import { Code } from "@/shared/ui/Code";
import type { Snooze } from "./types";

function formatTtl(ttl: number | undefined): string {
  if (ttl === undefined || ttl === 0) return "forever";
  if (ttl < 60) return `${ttl}s`;
  if (ttl < 3600) return `${Math.round(ttl / 60)}m`;
  if (ttl < 86400) return `${Math.round(ttl / 3600)}h`;
  return `${Math.round(ttl / 86400)}d`;
}

export const snoozeColumns: ColumnDef<Snooze>[] = [
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
    id: "ttl",
    header: "TTL",
    cell: (r) => formatTtl(r.ttl),
    sortable: true,
    width: "120px",
  },
  {
    id: "comment",
    header: "Comment",
    cell: (r) => <span style={{ color: "var(--text-muted)" }}>{r.comment ?? "—"}</span>,
  },
];
