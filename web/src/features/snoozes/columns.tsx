import type { ColumnDef } from "@/shared/ui/DataTable";
import { Badge, type BadgeVariant } from "@/shared/ui/Badge";
import { Code } from "@/shared/ui/Code";
import { prettyCondition } from "@/lib/condition/pretty";
import { secondsToHuman } from "@/lib/format/seconds";
import { TimeConstraintsCell } from "@/shared/ui/TimeConstraintsCell";
import type { Snooze } from "./types";

// Status badge styling per server-computed window_status: active reads as a
// healthy green, pending as a scheduled amber, expired as a quiet grey, and
// always-on as the reserved info blue (a permanent rule, never a countdown).
const STATUS_BADGE: Record<
  NonNullable<Snooze["window_status"]>,
  { variant: BadgeVariant; label: string }
> = {
  active: { variant: "ok", label: "active" },
  pending: { variant: "warning", label: "pending" },
  expired: { variant: "muted", label: "expired" },
  always_on: { variant: "info", label: "always on" },
};

// Remaining countdown. An always-on rule shows "forever"; a bounded active rule
// shows its time-to-close via secondsToHuman; anything else (no countdown:
// pending, expired, or a server too old to project the field) shows "—".
function remainingLabel(r: Snooze): string {
  if (r.enabled === false) return "—"; // a disabled rule is not counting down
  if (r.window_status === "always_on") return "forever";
  if (!r.remaining_seconds) return "—";
  return secondsToHuman(r.remaining_seconds);
}

export const snoozeColumns: ColumnDef<Snooze>[] = [
  {
    id: "window_status",
    header: "Status",
    cell: (r) => {
      // A disabled rule never suppresses, so its window lifecycle is moot —
      // say so plainly instead of showing a misleading "active"/"pending".
      if (r.enabled === false) return <Badge variant="muted">disabled</Badge>;
      if (!r.window_status) return <span style={{ color: "var(--text-muted)" }}>—</span>;
      const { variant, label } = STATUS_BADGE[r.window_status];
      return <Badge variant={variant}>{label}</Badge>;
    },
    width: "110px",
  },
  {
    id: "remaining_seconds",
    header: "Remaining",
    cell: (r) => (
      <span style={{ fontFamily: "var(--font-mono)", fontSize: "var(--text-xs)" }}>
        {remainingLabel(r)}
      </span>
    ),
    align: "right",
    width: "110px",
    hideBelow: "lg",
  },
  {
    id: "time_constraints",
    header: "Window",
    cell: (r) => <TimeConstraintsCell value={r.time_constraints} />,
    width: "210px",
    hideBelow: "xl",
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
    hideBelow: "xl",
  },
  {
    id: "hits",
    header: "Hits",
    cell: (r) => <span>{r.hits ?? 0}</span>,
    align: "right",
    width: "80px",
    hideBelow: "lg",
  },
  {
    id: "discard",
    header: "Discard",
    cell: (r) =>
      r.discard ? <Badge variant="warning">yes</Badge> : <Badge variant="muted">no</Badge>,
    width: "90px",
    hideBelow: "lg",
  },
];

export function snoozeRowDisabled(r: Snooze): boolean {
  return r.enabled === false;
}
