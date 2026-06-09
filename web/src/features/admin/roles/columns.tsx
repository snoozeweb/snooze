import { permissionBadgeVariant } from "@/lib/format/permission-color";
import { isPlatformRole } from "@/lib/format/role-color";
import type { ColumnDef } from "@/shared/ui/DataTable";
import { Badge } from "@/shared/ui/Badge";
import { Code } from "@/shared/ui/Code";
import type { Role } from "./types";
import styles from "./columns.module.css";

export const roleColumns: ColumnDef<Role>[] = [
  {
    id: "name",
    header: "Name",
    // The reserved platform_admin super-role gets the violet --role-platform
    // accent so it stands out from ordinary roles in the list.
    cell: (r) => (
      <Code {...(isPlatformRole(r.name) ? { className: styles.platformName } : {})}>{r.name}</Code>
    ),
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
            <Badge key={p} variant={permissionBadgeVariant(p)}>
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
