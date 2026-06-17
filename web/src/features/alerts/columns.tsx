import type { ColumnDef } from "@/shared/ui/DataTable";
import { Badge } from "@/shared/ui/Badge";
import { Code } from "@/shared/ui/Code";
import { TimeCell } from "@/shared/ui/TimeCell";
import { severityColor } from "@/lib/format/severity-color";
import { formatTTL, stateBadgeVariant, stateLabel } from "./format";
import type { AlertState, Record_ } from "./types";
import styles from "./columns.module.css";

// Records carry a `duplicates` counter (int64) bumped by the aggregate-rule
// plugin every time an incoming alert collapses into an existing row.
// internal/pluginimpl/aggregaterule/plugin.go lines ~216–244. Read-only.
function recordHits(r: Record_): number {
  const v = (r as { duplicates?: unknown }).duplicates;
  if (typeof v === "number") return v;
  if (typeof v === "string") {
    const n = Number(v);
    return Number.isFinite(n) ? n : 0;
  }
  return 0;
}

// `comment_count` is maintained server-side by the comment plugin
// (internal/pluginimpl/comment/plugin.go), incremented on every comment —
// including ack/close/esc state-changes. Not a typed Record field (records
// are dynamic), so read it defensively like `duplicates` above. Read-only.
export function recordCommentCount(r: Record_): number {
  const v = (r as { comment_count?: unknown }).comment_count;
  if (typeof v === "number") return v;
  if (typeof v === "string") {
    const n = Number(v);
    return Number.isFinite(n) ? n : 0;
  }
  return 0;
}

export const alertColumns: ColumnDef<Record_>[] = [
  {
    id: "date_epoch",
    header: "When",
    // TimeCell: same trimDate text as before, now mono-tabular with a full
    // timestamp tooltip and a "Nm ago" prefix while the alert is <1h old.
    // Width bumped 140→160px so the relative prefix doesn't wrap on fresh rows.
    cell: (r) => <TimeCell epoch={r.date_epoch} />,
    sortable: true,
    width: "160px",
  },
  {
    id: "severity",
    // Gradated per-severity tint from the dashboard palette (severityColor),
    // so e.g. emerg/crit/alert render as distinct red shades — matching the
    // dashboard's "By severity" chart instead of the flat variant buckets.
    header: "Sev",
    cell: (r) => <Badge color={severityColor(r.severity ?? "")}>{r.severity ?? "—"}</Badge>,
    sortable: true,
    width: "100px",
  },
  {
    id: "state",
    header: "State",
    cell: (r) => {
      const state = (r.state ?? "") as AlertState;
      return <Badge variant={stateBadgeVariant(state)}>{stateLabel(state)}</Badge>;
    },
    sortable: true,
    width: "120px",
  },
  {
    id: "hits",
    header: "Hits",
    cell: (r) => {
      const n = recordHits(r);
      return n > 1 ? <Badge variant="muted">×{n}</Badge> : <span>—</span>;
    },
    align: "right",
    width: "80px",
  },
  {
    id: "host",
    header: "Host",
    cell: (r) => <Code>{r.host ?? ""}</Code>,
    sortable: true,
    width: "160px",
  },
  {
    // Process column sits between host and source, mirroring the field
    // order from old snooze's src/snooze/defaults/web/alert.yaml.
    id: "process",
    header: "Process",
    cell: (r) => (r.process ? <Code>{r.process}</Code> : <span>—</span>),
    sortable: true,
    width: "120px",
  },
  {
    id: "source",
    header: "Source",
    cell: (r) => r.source ?? "—",
    width: "120px",
  },
  {
    id: "environment",
    header: "Environment",
    cell: (r) => r.environment ?? "—",
    width: "140px",
  },
  {
    // TTL column — surfaces the same lifecycle hint old snooze used: how
    // long until the alert is auto-cleaned by the housekeeper, or
    // "shelved" / "expired". Records ingested without a ttl render as "—"
    // (the server stamps the default at ingest, so this only triggers for
    // pre-existing rows from before the stamping change).
    id: "ttl",
    header: "TTL",
    cell: (r) => <span>{formatTTL(r.ttl, r.date_epoch)}</span>,
    width: "120px",
  },
  {
    id: "message",
    header: "Message",
    cell: (r) => <span className={styles.message}>{r.message ?? ""}</span>,
  },
];
