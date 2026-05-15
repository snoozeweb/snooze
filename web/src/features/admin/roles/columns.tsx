import type { ColumnDef } from "@/shared/ui/DataTable";
import { Badge, type BadgeVariant } from "@/shared/ui/Badge";
import { Code } from "@/shared/ui/Code";
import type { Role } from "./types";

// Hint at what the permission grants. rw_* (full read+write) → critical,
// ro_* → info, audit/admin-only → warning, deny_* → muted.
function permissionVariant(p: string): BadgeVariant {
  if (p === "rw_all" || p.startsWith("admin_")) return "critical";
  if (p.startsWith("rw_") || p.startsWith("can_")) return "warning";
  if (p.startsWith("ro_")) return "info";
  if (p.startsWith("deny_") || p === "anonymous") return "muted";
  return "neutral";
}

export const roleColumns: ColumnDef<Role>[] = [
  {
    id: "name",
    header: "Name",
    cell: (r) => <Code>{r.name}</Code>,
    sortable: true,
    width: "200px",
  },
  {
    id: "permissions",
    header: "Permissions",
    cell: (r) => {
      const perms = r.permissions ?? [];
      if (perms.length === 0) return <span style={{ color: "var(--text-muted)" }}>—</span>;
      return (
        <span style={{ display: "inline-flex", gap: "var(--space-1)", flexWrap: "wrap" }}>
          {perms.map((p) => (
            <Badge key={p} variant={permissionVariant(p)}>
              {p}
            </Badge>
          ))}
        </span>
      );
    },
  },
  {
    id: "comment",
    header: "Comment",
    cell: (r) => <span style={{ color: "var(--text-muted)" }}>{r.comment ?? "—"}</span>,
    width: "240px",
  },
];
