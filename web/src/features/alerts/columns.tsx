import type { ColumnDef } from "@/shared/ui/DataTable";
import { Badge } from "@/shared/ui/Badge";
import { Code } from "@/shared/ui/Code";
import { TimeCell } from "@/shared/ui/TimeCell";
import { severityColor } from "@/lib/format/severity-color";
import {
  formatCountdown,
  formatShelveUntil,
  formatTTL,
  stateBadgeVariant,
  stateLabel,
  trendLabel,
} from "./format";
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

/** Reads shelve_until defensively (dynamic field from Plan 34 backend). */
export function recordShelveUntil(r: Record_): number {
  const v = (r as { shelve_until?: unknown }).shelve_until;
  if (typeof v === "number") return v;
  if (typeof v === "string") {
    const n = Number(v);
    return Number.isFinite(n) ? n : 0;
  }
  return 0;
}

// `acked_by` is denormalised onto the record by the comment plugin
// (internal/pluginimpl/comment/plugin.go AfterCreate): set to the operator's
// login on ack, removed on open/close, kept through esc. Read defensively —
// it's absent on records never acked in their current lifecycle. Read-only.
export function recordAckedBy(r: Record_): string {
  const v = (r as { acked_by?: unknown }).acked_by;
  return typeof v === "string" ? v : "";
}

// `trend_indication` is not yet in the OpenAPI schema (Plan 30 writes it as an
// extra doc field). Read defensively. Once Plan 30's OpenAPI regeneration adds
// the typed field to types.gen.ts, this cast can be replaced with r.trend_indication.
function recordTrend(r: Record_): "moreSevere" | "lessSevere" | "noChange" | "" {
  const v = (r as { trend_indication?: unknown }).trend_indication;
  if (v === "moreSevere" || v === "lessSevere" || v === "noChange") return v;
  return "";
}

// Column width budget: under `table-layout: fixed`, the sum of every VISIBLE
// column's fixed width (plus the 12px quick-actions spacer, the kebab's
// 36px, and cell padding, which counts toward width under border-box) must
// leave the flexible Message column >=~180px at the lower edge of each tier
// band — otherwise Message (and the quick-actions overlay that floats over
// its tail) get squeezed toward zero. Hidden columns aren't gone: they
// reappear in the detail drawer, and in card mode every field reflows back
// in regardless of tier.
export const alertColumns: ColumnDef<Record_>[] = [
  {
    id: "date_epoch",
    header: "When",
    // TimeCell: same trimDate text as before, now mono-tabular with a full
    // timestamp tooltip and a "Nm ago" prefix while the alert is <1h old.
    cell: (r) => <TimeCell epoch={r.date_epoch} />,
    sortable: true,
    width: "210px",
  },
  {
    id: "severity",
    // Gradated per-severity tint from the dashboard palette (severityColor),
    // so e.g. emerg/crit/alert render as distinct red shades — matching the
    // dashboard's "By severity" chart instead of the flat variant buckets.
    // The trend arrow (↑/↓, reflecting trend_indication stamped by
    // aggregaterule) rides alongside the badge instead of its own column —
    // it only renders for an actual severity change; noChange/absent shows
    // nothing rather than a "—" placeholder that ate a whole column before.
    header: "Sev",
    cell: (r) => {
      const t = recordTrend(r);
      return (
        <span className={styles.cell}>
          <Badge className={styles.badgeText!} color={severityColor(r.severity ?? "")}>
            {r.severity ?? "—"}
          </Badge>
          {t === "moreSevere" ? (
            <span
              className={styles.trend}
              title={trendLabel("moreSevere")}
              aria-label={trendLabel("moreSevere")}
            >
              ↑
            </span>
          ) : t === "lessSevere" ? (
            <span
              className={styles.trend}
              title={trendLabel("lessSevere")}
              aria-label={trendLabel("lessSevere")}
            >
              ↓
            </span>
          ) : null}
        </span>
      );
    },
    sortable: true,
    width: "130px",
  },
  {
    id: "state",
    header: "State",
    // The auto-escalation countdown rides as a muted hint alongside the badge
    // on open-ish rows (compact: "in 1d 23h"). Who-acked/ack-expiry does NOT —
    // it never fit legibly in a column-width hint; the detail drawer carries it.
    cell: (r) => {
      const state = (r.state ?? "") as AlertState;
      const isOpenish = state === "" || state === "open" || state === "esc";
      const hint = isOpenish ? formatCountdown(r.escalate_at) : "";
      return (
        <span className={styles.cell}>
          <Badge className={styles.badgeText!} variant={stateBadgeVariant(state)}>
            {stateLabel(state)}
          </Badge>
          {hint ? (
            <span className={styles.hint} title="Auto-escalation deadline">
              {hint}
            </span>
          ) : null}
        </span>
      );
    },
    sortable: true,
    width: "110px",
  },
  {
    id: "hits",
    header: "Hits",
    cell: (r) => {
      const n = recordHits(r);
      return n > 1 ? <Badge variant="muted">×{n}</Badge> : <span>—</span>;
    },
    align: "right",
    width: "77px",
    hideBelow: "lg",
  },
  {
    id: "host",
    header: "Host",
    cell: (r) => (r.host ? <Code>{r.host}</Code> : <span>—</span>),
    sortable: true,
    width: "150px",
    hideBelow: "md",
  },
  {
    // Process column sits between host and source, mirroring the field
    // order from old snooze's src/snooze/defaults/web/alert.yaml.
    id: "process",
    header: "Process",
    cell: (r) => (r.process ? <Code>{r.process}</Code> : <span>—</span>),
    sortable: true,
    width: "203px",
    hideBelow: "xl",
  },
  {
    id: "source",
    header: "Source",
    cell: (r) => r.source ?? "—",
    width: "120px",
    hideBelow: "xl",
  },
  {
    id: "environment",
    header: "Environment",
    cell: (r) => r.environment ?? "—",
    width: "120px",
    hideBelow: "xxl",
  },
  {
    // TTL column — surfaces the same lifecycle hint old snooze used: how
    // long until the alert is auto-cleaned by the housekeeper, or
    // "shelved" / "expired". Records ingested without a ttl render as "—"
    // (the server stamps the default at ingest, so this only triggers for
    // pre-existing rows from before the stamping change).
    id: "ttl",
    header: "TTL",
    cell: (r) => {
      const su = recordShelveUntil(r);
      const label = su > 0 ? formatShelveUntil(su) : formatTTL(r.ttl, r.date_epoch);
      return <span>{label}</span>;
    },
    width: "100px",
    hideBelow: "xxl",
  },
  {
    id: "message",
    header: "Message",
    cell: (r) => <span className={styles.message}>{r.message || "—"}</span>,
  },
];
