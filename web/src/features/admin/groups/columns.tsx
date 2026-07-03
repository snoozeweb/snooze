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
    id: "description",
    header: "Description",
    cell: (r) => (
      <span style={{ color: r.description ? "inherit" : "var(--text-muted)" }}>
        {r.description ?? "—"}
      </span>
    ),
  },
  {
    id: "members",
    header: "Members",
    cell: (r) => {
      const count = (r.members ?? []).length;
      if (count === 0) return <span style={{ color: "var(--text-muted)" }}>—</span>;
      return (
        <Badge variant="neutral">
          {count} {count === 1 ? "member" : "members"}
        </Badge>
      );
    },
    width: "120px",
  },
];
