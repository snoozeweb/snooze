import type { ColumnDef } from "@/shared/ui/DataTable";
import { Badge } from "@/shared/ui/Badge";
import { Code } from "@/shared/ui/Code";
import type { Group } from "./types";

export const groupColumns: ColumnDef<Group>[] = [
  {
    id: "name",
    header: "Name",
    cell: (r) => <Code>{r.name}</Code>,
    sortable: true,
    width: "200px",
  },
  {
    id: "members",
    header: "Members",
    // Name every member as a username chip (rather than a bare count) so the
    // table answers "who is in this group?" at a glance. The auth backend of
    // each member rides along as a native title tooltip — enough to tell a local
    // "alice" from an LDAP "alice" without cluttering the row.
    cell: (r) => {
      const members = r.members ?? [];
      if (members.length === 0) return <span style={{ color: "var(--text-muted)" }}>—</span>;
      return (
        <span style={{ display: "inline-flex", gap: "var(--space-1)", flexWrap: "wrap" }}>
          {members.map((m) => (
            <span key={`${m.method}:${m.username}`} title={`${m.username} · ${m.method}`}>
              {/* title="" opts out of Badge's own default so hovering shows
                  this wrapper's fuller "username · method" tooltip. */}
              <Badge variant="neutral" title="">
                {m.username}
              </Badge>
            </span>
          ))}
        </span>
      );
    },
  },
  {
    id: "description",
    header: "Description",
    cell: (r) => (
      <span style={{ color: r.description ? "inherit" : "var(--text-muted)" }}>
        {r.description ?? "—"}
      </span>
    ),
    width: "280px",
  },
];
