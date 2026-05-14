import type { ColumnDef } from "@/shared/ui/DataTable";
import { Code } from "@/shared/ui/Code";
import type { Setting } from "./types";

export const settingColumns: ColumnDef<Setting>[] = [
  {
    id: "name",
    header: "Name",
    cell: (r) => <Code>{r.name}</Code>,
    sortable: true,
    width: "260px",
  },
  {
    id: "value",
    header: "Value",
    cell: (r) => {
      const v = JSON.stringify(r.value ?? null);
      const display = v.length > 80 ? v.slice(0, 77) + "…" : v;
      return (
        <span style={{ fontFamily: "var(--font-mono)", fontSize: "var(--text-xs)" }}>
          {display}
        </span>
      );
    },
  },
  {
    id: "comment",
    header: "Comment",
    cell: (r) => <span style={{ color: "var(--text-muted)" }}>{r.comment ?? "—"}</span>,
  },
];
