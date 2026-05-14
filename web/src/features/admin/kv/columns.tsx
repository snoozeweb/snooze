import type { ColumnDef } from "@/shared/ui/DataTable";
import { Code } from "@/shared/ui/Code";
import type { KV } from "./types";

export const kvColumns: ColumnDef<KV>[] = [
  {
    id: "key",
    header: "Key",
    cell: (r) => <Code>{r.key}</Code>,
    sortable: true,
    width: "240px",
  },
  {
    id: "value",
    header: "Value",
    cell: (r) => {
      const v = r.value ?? "";
      const display = v.length > 80 ? v.slice(0, 77) + "…" : v;
      return (
        <span style={{ fontFamily: "var(--font-mono)", fontSize: "var(--text-xs)" }}>
          {display || "—"}
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
