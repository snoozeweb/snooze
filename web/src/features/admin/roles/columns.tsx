import { permissionBadgeVariant } from "@/lib/format/permission-color";
import { permissionDescription } from "@/lib/format/permission-info";
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
          {perms.map((p) => {
            // Native title (not the app Tooltip) so the read-only cell carries a
            // hover explanation without depending on a TooltipProvider ancestor.
            const desc = permissionDescription(p);
            return (
              <span key={p} style={{ display: "inline-flex" }} {...(desc ? { title: desc } : {})}>
                <Badge variant={permissionBadgeVariant(p)}>{p}</Badge>
              </span>
            );
          })}
        </span>
      );
    },
  },
  {
    id: "groups",
    header: "Groups",
    cell: (r) => {
      const groups = r.groups ?? [];
      if (groups.length === 0) return <span style={{ color: "var(--text-muted)" }}>—</span>;
      return (
        <span style={{ display: "inline-flex", gap: "var(--space-1)", flexWrap: "wrap" }}>
          {groups.map((g) => (
            <Badge key={g} variant="neutral">
              {g}
            </Badge>
          ))}
        </span>
      );
    },
    width: "200px",
  },
  {
    id: "comment",
    header: "Comment",
    cell: (r) => <span style={{ color: "var(--text-muted)" }}>{r.comment ?? "—"}</span>,
    width: "240px",
  },
];
