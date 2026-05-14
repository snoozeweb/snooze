import type { ColumnDef } from "@/shared/ui/DataTable";
import { Code } from "@/shared/ui/Code";
import type { Environment } from "./types";

export const environmentColumns: ColumnDef<Environment>[] = [
  {
    id: "name",
    header: "Name",
    cell: (r) => <Code>{r.name}</Code>,
    sortable: true,
    width: "240px",
  },
  {
    id: "color",
    header: "Color",
    width: "100px",
    cell: (r) =>
      r.color ? (
        <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
          <span
            style={{
              width: 12,
              height: 12,
              borderRadius: 2,
              background: r.color,
              border: "1px solid var(--border)",
            }}
          />
          <span>{r.color}</span>
        </span>
      ) : (
        <span style={{ color: "var(--text-muted)" }}>—</span>
      ),
  },
  {
    id: "comment",
    header: "Comment",
    cell: (r) => <span style={{ color: "var(--text-muted)" }}>{r.comment ?? "—"}</span>,
  },
];
