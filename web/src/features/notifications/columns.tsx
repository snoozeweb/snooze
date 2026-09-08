import type { ColumnDef } from "@/shared/ui/DataTable";
import { Badge } from "@/shared/ui/Badge";
import { Code } from "@/shared/ui/Code";
import { prettyCondition } from "@/lib/condition/pretty";
import { summarizeFrequency } from "@/shared/ui/frequencyUtils";
import { TimeCell } from "@/shared/ui/TimeCell";
import { TimeConstraintsCell } from "@/shared/ui/TimeConstraintsCell";
import type { Action, Notification } from "./types";
import styles from "./columns.module.css";

export const notificationColumns: ColumnDef<Notification>[] = [
  {
    id: "time_constraints",
    header: "Window",
    cell: (r) => <TimeConstraintsCell value={r.time_constraints} />,
    width: "210px",
    hideBelow: "lg",
    cardRole: "meta",
  },
  {
    // The row's identifier — the card's headline (see alerts' `message`
    // column for the same "body" precedent).
    id: "name",
    header: "Name",
    cell: (r) => <Code>{r.name}</Code>,
    sortable: true,
    width: "200px",
    cardRole: "body",
  },
  {
    id: "condition",
    header: "Condition",
    cell: (r) => (
      <span style={{ fontFamily: "var(--font-mono)", fontSize: "var(--text-xs)" }}>
        {prettyCondition(r.condition)}
      </span>
    ),
    cardRole: "meta",
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
    cardRole: "meta",
  },
  {
    // Server-stamped counters (D10). The column ids are the record field
    // names on purpose: `serverSort` forwards `sortBy` straight to the CRUD
    // `orderby`, so `id` IS the sort key.
    //
    // cardRole "header" (W18): below the 640px card breakpoint these render
    // as a compact unlabelled chip on the card's first line instead of a
    // full `label — value` row. A notification that never sent renders
    // NOTHING (not an em-dash) — matching alerts' `hits` column — because
    // DataTable's card CSS only collapses a "header" slot when its content
    // is truly empty (`td[data-card="header"]:has(> .cellInner:empty)`);
    // an em-dash placeholder would still count as content and show up as
    // noise on every never-sent notification's card.
    id: "hits",
    header: "Sent",
    cell: (r) => (r.hits && r.hits > 0 ? <span className={styles.count}>{r.hits}</span> : null),
    sortable: true,
    align: "right",
    width: "80px",
    hideBelow: "md",
    cardRole: "header",
  },
  {
    id: "last_sent",
    header: "Last sent",
    // See the `hits` comment above: render nothing (rather than TimeCell's
    // own "—" fallback) so the card omits this slot entirely when a
    // notification has never sent.
    cell: (r) => (r.last_sent ? <TimeCell epoch={r.last_sent} compact /> : null),
    sortable: true,
    width: "110px",
    hideBelow: "lg",
    cardRole: "header",
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
    hideBelow: "xl",
    cardRole: "meta",
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
    hideBelow: "xl",
    cardRole: "meta",
  },
];

export function notificationRowDisabled(r: Notification): boolean {
  return r.enabled === false;
}

// summarizeSubcontent picks 1-3 short hints from the subcontent map so the
// "Action" column reads at a glance ("url=…", "command=…", "host=…") instead
// of just showing the plugin name twice. Mirrors the Python "pprint" cell.
// Non-secret identifying fields, broadened well beyond the original 6 so the
// column actually summarizes the integrations ops teams use (teams/discord/
// mattermost webhook_url, jira jira_url/project_key, opsgenie region/priority,
// ntfy topic, telegram chat_id, mail to/from, …). Deliberately excludes
// credential-shaped keys — we never render tokens/passwords/keys in the table.
const SUMMARY_KEYS = [
  "url",
  "webhook_url",
  "jira_url",
  "project_key",
  "command",
  "script",
  "host",
  "to",
  "from",
  "channel",
  "room",
  "topic",
  "chat_id",
  "region",
  "priority",
  "email",
  "sender",
];

export function summarizeSubcontent(sub: Record<string, unknown> | undefined): string {
  if (!sub) return "";
  const parts: string[] = [];
  for (const key of SUMMARY_KEYS) {
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
    hideBelow: "lg",
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
    hideBelow: "xl",
  },
];
