import type { ColumnDef } from "@/shared/ui/DataTable";
import { Badge } from "@/shared/ui/Badge";
import { Code } from "@/shared/ui/Code";
import { prettyCondition } from "@/lib/condition/pretty";
import { summarizeFrequency } from "@/shared/ui/frequencyUtils";
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

// summarizeSubcontent picks 1-3 short hints from the subcontent map so the
// "Action" column reads at a glance ("url=…", "command=…", "host=…") instead
// of just showing the plugin name twice. Mirrors the Python "pprint" cell.
function summarizeSubcontent(sub: Record<string, unknown> | undefined): string {
  if (!sub) return "";
  const preferred = ["url", "command", "script", "host", "to", "channel"];
  const parts: string[] = [];
  for (const key of preferred) {
    const v = sub[key];
    if (v === undefined || v === null || v === "") continue;
    parts.push(`${key}=${shortValue(v)}`);
    if (parts.length === 2) break;
  }
  return parts.join(" • ");
}

function shortValue(v: unknown): string {
  if (Array.isArray(v)) {
    return v
      .slice(0, 3)
      .map((x) => (typeof x === "string" ? x : JSON.stringify(x)))
      .join(" ");
  }
  if (typeof v === "object") return "{…}";
  if (typeof v === "string") return v.length > 60 ? v.slice(0, 60) + "…" : v;
  if (typeof v === "number" || typeof v === "boolean") return String(v);
  return "";
}

export const actionColumns: ColumnDef<Action>[] = [
  {
    id: "name",
    header: "Name",
    cell: (r) => <Code>{r.name}</Code>,
    sortable: true,
    width: "200px",
  },
  {
    id: "selected",
    header: "Type",
    // `action.selected` is the notifier plugin (mail / webhook / …). The
    // backend wire shape nests it under `action`, mirroring the Python
    // ActionObject layout. See pluginimpl/notification/plugin.go.
    cell: (r) => <Badge variant="neutral">{r.action?.selected ?? "—"}</Badge>,
    width: "120px",
  },
  {
    id: "action",
    header: "Action",
    cell: (r) => {
      const s = summarizeSubcontent(r.action?.subcontent);
      return s ? (
        <span style={{ fontFamily: "var(--font-mono)", fontSize: "var(--text-xs)" }}>{s}</span>
      ) : (
        <span style={{ color: "var(--text-muted)" }}>—</span>
      );
    },
  },
  {
    id: "comment",
    header: "Comment",
    cell: (r) => <span style={{ color: "var(--text-muted)" }}>{r.comment ?? "—"}</span>,
    width: "240px",
  },
  {
    id: "batch",
    header: "Batch",
    // Batch lives in subcontent.batch (the notifier plugins read it via
    // NotificationPayload.Meta). Surface as a yes/no badge to match the
    // notification table's batch column.
    cell: (r) =>
      r.action?.subcontent?.batch === true ? (
        <Badge variant="info">yes</Badge>
      ) : (
        <Badge variant="muted">no</Badge>
      ),
    width: "80px",
  },
];
