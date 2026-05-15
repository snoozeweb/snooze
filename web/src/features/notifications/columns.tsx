import type { ColumnDef } from "@/shared/ui/DataTable";
import { Badge } from "@/shared/ui/Badge";
import { Code } from "@/shared/ui/Code";
import { prettyCondition } from "@/lib/condition/pretty";
import { summarizeFrequency } from "@/shared/ui/FrequencyEditor";
import { TimeConstraintsCell } from "@/shared/ui/TimeConstraintsCell";
import type { Action, Notification } from "./types";

export const notificationColumns: ColumnDef<Notification>[] = [
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
    id: "actions",
    header: "Actions",
    cell: (r) => (
      <span style={{ display: "inline-flex", gap: "var(--space-1)", flexWrap: "wrap" }}>
        {(r.actions ?? []).map((a) => (
          <Badge key={a} variant="info">
            {a}
          </Badge>
        ))}
        {(r.actions ?? []).length === 0 ? (
          <span style={{ color: "var(--text-muted)" }}>—</span>
        ) : null}
      </span>
    ),
    width: "200px",
  },
  {
    id: "frequency",
    header: "Frequency",
    cell: (r) => (
      <span style={{ color: "var(--text-muted)", fontSize: "var(--text-xs)" }}>
        {summarizeFrequency(r.frequency)}
      </span>
    ),
    width: "160px",
  },
  {
    id: "batch",
    header: "Batch",
    // Backend doesn't expose a separate "batch" boolean — frequency.total>1
    // is the semantic signal (one delivery may carry many alerts). Surface
    // it as a yes/no badge so the table reads at a glance.
    cell: (r) =>
      (r.frequency?.total ?? 0) > 1 ? (
        <Badge variant="info">yes</Badge>
      ) : (
        <Badge variant="muted">no</Badge>
      ),
    width: "80px",
  },
];

export function notificationRowDisabled(r: Notification): boolean {
  return r.enabled === false;
}

export const actionColumns: ColumnDef<Action>[] = [
  {
    id: "name",
    header: "Name",
    cell: (r) => <Code>{r.name}</Code>,
    sortable: true,
    width: "240px",
  },
  {
    id: "action_type",
    header: "Type",
    cell: (r) => <Badge variant="neutral">{r.action_type ?? "—"}</Badge>,
    sortable: true,
    width: "140px",
  },
  {
    id: "comment",
    header: "Comment",
    cell: (r) => <span style={{ color: "var(--text-muted)" }}>{r.comment ?? "—"}</span>,
  },
];
