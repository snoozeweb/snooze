import type { ColumnDef } from "@/shared/ui/DataTable";
import { Badge } from "@/shared/ui/Badge";
import { Code } from "@/shared/ui/Code";
import { roleBadgeVariant } from "@/lib/format/role-color";
import { formatRelativeTime } from "@/features/alerts/format";
import type { User } from "./types";

// eslint-disable-next-line react-refresh/only-export-components
function BadgeList({ items, palette }: { items: string[] | undefined; palette?: (s: string) => string }) {
  if (!items || items.length === 0)
    return <span style={{ color: "var(--text-muted)" }}>—</span>;
  return (
    <span style={{ display: "inline-flex", gap: "var(--space-1)", flexWrap: "wrap" }}>
      {items.map((v) => {
        const variant = palette ? palette(v) : "neutral";
        return (
          <Badge key={v} variant={variant as never}>
            {v}
          </Badge>
        );
      })}
    </span>
  );
}

export const userColumns: ColumnDef<User>[] = [
  {
    id: "name",
    header: "Name",
    cell: (r) => <Code>{r.name}</Code>,
    sortable: true,
    width: "180px",
  },
  {
    id: "type",
    header: "Type",
    cell: (r) => <Badge variant="neutral">{r.method ?? r.type ?? "local"}</Badge>,
    width: "90px",
  },
  {
    id: "roles",
    header: "Roles",
    cell: (r) => <BadgeList items={r.roles} palette={roleBadgeVariant} />,
    width: "240px",
  },
  {
    id: "groups",
    header: "Groups",
    cell: (r) => <BadgeList items={r.groups} />,
    width: "200px",
  },
  {
    id: "last_login",
    header: "Last login",
    cell: (r) =>
      r.last_login ? (
        <span style={{ color: "var(--text-muted)" }}>{formatRelativeTime(r.last_login)}</span>
      ) : (
        <span style={{ color: "var(--text-muted)" }}>never</span>
      ),
    width: "120px",
  },
  {
    id: "comment",
    header: "Comment",
    cell: (r) => <span style={{ color: "var(--text-muted)" }}>{r.comment ?? "—"}</span>,
  },
];
