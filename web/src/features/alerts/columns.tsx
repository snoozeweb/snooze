import type { ColumnDef } from "@/shared/ui/DataTable";
import { Badge } from "@/shared/ui/Badge";
import { Code } from "@/shared/ui/Code";
import { TimeCell } from "@/shared/ui/TimeCell";
import { severityColor } from "@/lib/format/severity-color";
import { MessageCell } from "./MessageCell";
import {
  formatCountdown,
  formatShelveUntil,
  formatTTL,
  severityDisplayLabel,
  stateBadgeVariant,
  stateLabel,
  trendLabel,
} from "./format";
import type { AlertState, Record_ } from "./types";
import styles from "./columns.module.css";

// Records carry a `duplicates` counter (int64) stamped by the aggregate-rule
// plugin: 1 on the first occurrence, prev+1 every time an incoming alert
// collapses into an existing row (internal/pluginimpl/aggregaterule/plugin.go,
// the single bump site around line 466). So 1 (or absent, on records written
// before the counter existed) means "this happened once" — not "unknown".
// Read-only.
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
// column's fixed width (plus the 12px quick-actions spacer, the kebab's 36px,
// the 32px checkbox, and cell padding, which counts toward width under
// border-box) is subtracted from the container to size the one flexible
// column — Message. The tiers below are set so Message clears ~480px at every
// realistic desktop width and never drops under ~240px on a tablet:
//
//   container   hidden tiers      fixed+controls   Message
//   1600px      —                 810px            790px
//   1280px      xxl               714px            566px
//   1150px      xxl+xl            594px            556px   (1440px window)
//   1000px      xxl+xl+lg         530px            470px   (1280px window)
//    768px      xxl+xl+lg         530px            238px
//    640px      → card mode (cardRole decides placement, tiers stop applying)
//
// Hidden columns aren't gone: they reappear in the detail drawer, and on a
// phone `cardRole` decides what the card shows.
//
// Order is the SERVER's, not this array's: AlertsPage runs the list through
// `columnsForConfig(config.columns)`, so the default order lives in
// internal/api/routes_config.go `defaultColumns` (mirrored in
// features/config/types.ts CONSOLE_FALLBACK and the settings metadata) and
// this array only has to agree with it. `process` and `source` are defined
// here but absent from that default list — an operator who wants them back
// adds the id under Settings → Console → "Alert table columns".
export const alertColumns: ColumnDef<Record_>[] = [
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
          <Badge
            className={styles.badgeText!}
            color={severityColor(r.severity ?? "")}
            title={r.severity ?? "—"}
          >
            {r.severity ? severityDisplayLabel(r.severity) : "—"}
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
    width: "100px",
    cardRole: "header",
  },
  {
    // Position 2, and the only flexible column: what broke is the reason the
    // operator opened this page, so it gets the remainder of the width and a
    // two-line clamp (see MessageCell and the budget table above).
    id: "message",
    header: "Message",
    cell: (r) => (r.message ? <MessageCell text={r.message} /> : <span>—</span>),
    cardRole: "body",
  },
  {
    // The noise-reduction number, and the one column that says the product's
    // thesis out loud. `duplicates` is 1 for an alert that happened once, so a
    // singleton renders NOTHING — an em-dash on every row (what this column
    // used to be) reads as "no data" and buries the rows that do repeat.
    id: "hits",
    header: "Hits",
    cell: (r) => {
      const n = recordHits(r);
      if (n <= 1) return null;
      const label = `${n} duplicate events aggregated into this alert`;
      return (
        <span className={styles.hits} title={label} aria-label={label}>
          <Badge variant="muted">×{n}</Badge>
        </span>
      );
    },
    align: "right",
    // 64px (minus the cell's 20px of horizontal padding) only fit ~3 digits
    // before the badge got clipped by the ancestor `.row td` overflow:hidden
    // — a 5-digit aggregate count (×12345) was cropped. 88px covers up to 7
    // digits with room to spare.
    width: "88px",
    hideBelow: "lg",
    cardRole: "header",
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
    // 112px fit the old "Escalated"/"Ack" labels on one line; the canonical
    // "Acknowledged"/"Re-escalated" nouns are longer and wrapped to two
    // lines at that width even though .badgeText tolerates the wrap
    // (nothing clips) — widened just enough to keep them on one line.
    width: "136px",
    cardRole: "header",
  },
  {
    id: "date_epoch",
    header: "When",
    // One relative value ("4m ago", "3d ago") with the absolute timestamp on
    // hover. The old pairing ("4m ago  Today 14:32") spent 210px saying the
    // same thing twice; triage reads the age, and the ~120px this frees goes
    // to the message.
    cell: (r) => <TimeCell epoch={r.date_epoch} compact />,
    sortable: true,
    width: "88px",
    cardRole: "header",
  },
  {
    id: "host",
    header: "Host",
    cell: (r) => (r.host ? <Code>{r.host}</Code> : <span>—</span>),
    sortable: true,
    width: "150px",
    hideBelow: "md",
    cardRole: "meta",
  },
  {
    id: "environment",
    header: "Environment",
    cell: (r) => r.environment ?? "—",
    width: "120px",
    hideBelow: "xl",
    cardRole: "hidden",
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
    width: "96px",
    hideBelow: "xxl",
    cardRole: "hidden",
  },
  {
    // Process and Source: defined, but off the default column list (see the
    // budget note above). They were costing 320px of a desktop row to repeat
    // what host+message already say, and both are one click away in the row
    // inspector's Record tab.
    id: "process",
    header: "Process",
    cell: (r) => (r.process ? <Code>{r.process}</Code> : <span>—</span>),
    sortable: true,
    width: "180px",
    hideBelow: "xl",
    cardRole: "hidden",
  },
  {
    id: "source",
    header: "Source",
    cell: (r) => r.source ?? "—",
    width: "110px",
    hideBelow: "xl",
    cardRole: "hidden",
  },
];
