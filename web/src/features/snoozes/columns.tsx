import type { ColumnDef } from "@/shared/ui/DataTable";
import { Badge } from "@/shared/ui/Badge";
import { Code } from "@/shared/ui/Code";
import { prettyCondition } from "@/lib/condition/pretty";
import { TimeConstraintsCell } from "@/shared/ui/TimeConstraintsCell";
import type { Snooze } from "./types";

export const snoozeColumns: ColumnDef<Snooze>[] = [
  {
    id: "time_constraints",
    header: "Window",
    cell: (r) => <TimeConstraintsCell value={r.time_constraints} />,
    width: "210px",
  },
  {
    id: "name",
    header: "Name",
    cell: (r) => <Code>{r.name}</Code>,
    sortable: true,
    width: "200px",
  },
  {
    id: "condition",
    header: "Condition",
    cell: (r) => (
      <span style={{ fontFamily: "var(--font-mono)", fontSize: "var(--text-xs)" }}>
        {prettyCondition(r.condition)}
      </span>
    ),
  },
  {
    id: "user",
    header: "User",
    cell: (r) => <span style={{ color: "var(--text-muted)" }}>{r.name_create ?? "—"}</span>,
    width: "120px",
  },
  {
    id: "hits",
    header: "Hits",
    cell: (r) => <span>{r.hits ?? 0}</span>,
    align: "right",
    width: "80px",
  },
  {
    id: "discard",
    header: "Discard",
    cell: (r) =>
      r.discard ? <Badge variant="warning">yes</Badge> : <Badge variant="muted">no</Badge>,
    width: "90px",
  },
];

export function snoozeRowDisabled(r: Snooze): boolean {
  return r.enabled === false;
}
