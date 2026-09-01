// AlertFlowChart — the pipeline path a single alert actually took:
//   input → rules → aggregate → (snooze, terminal) | notifications → actions
// All data comes from the record row; no fetch. Colours via Badge variants only.
// Every entity (rule, aggregate, snooze, notification, action) deep-links to
// its management page with the page's search filter pre-set to the clicked
// object by name — the same URL contract the dashboard drill-downs and
// ActivityFeed use (see useTableSearch / SearchBar). Navigating to a page with
// ?search=name = "X" lands with that filter already applied.
import { Fragment, type ReactNode } from "react";
import { Link } from "@tanstack/react-router";
import { Badge, type BadgeVariant } from "@/shared/ui/Badge";
import { Tooltip } from "@/shared/ui/Tooltip";
import type { Record_ } from "./types";
import styles from "./AlertFlowChart.module.css";

type ActionResult = NonNullable<Record_["actions"]>[number];

const ACTION_VARIANT: Record<string, BadgeVariant> = {
  success: "ok",
  error: "error",
  skipped: "muted",
  pending: "warning",
  sent: "neutral",
};

const ACTION_GLYPH: Record<string, string> = {
  success: "✓",
  error: "✗",
  skipped: "⊘",
  pending: "⏳",
  sent: "➤",
};

const ACTION_HINT: Record<string, string> = {
  skipped: "Skipped — notification frequency is off",
  pending: "Send in progress",
  sent: "Dispatched — outcome tracking disabled",
};

// nameQuery encodes a value into a search-DSL equality on `name`, e.g.
// `name = "web-01"`. Backslash and double-quote are escaped to match the
// lexer's string rules (shared/searchdsl/lexer.ts) so names containing spaces
// or quotes still round-trip through the target page's SearchBar cleanly.
function nameQuery(value: string): string {
  const escaped = value.replace(/\\/g, "\\\\").replace(/"/g, '\\"');
  return `name = "${escaped}"`;
}

function Node({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className={styles.node}>
      <div className={styles.nodeLabel}>{label}</div>
      <div className={styles.nodeBody}>{children}</div>
    </div>
  );
}

function Connector() {
  return <div className={styles.connector} aria-hidden="true" />;
}

function ActionChip({ action }: { action: ActionResult }) {
  const status = action.status ?? "sent";
  const variant = ACTION_VARIANT[status] ?? "neutral";
  const glyph = ACTION_GLYPH[status] ?? "";
  const badge = (
    <Badge variant={variant}>
      <span aria-hidden="true">{glyph}</span> {action.name}
    </Badge>
  );
  // The error message and the sent/skipped hints are both surfaced as a hover
  // tooltip, which frees the chip's click to deep-link to the Actions page.
  const tip = status === "error" && action.error ? action.error : ACTION_HINT[status];

  // An action with no name can't be deep-linked — render the badge as plain
  // text (with its tooltip, if any) so nothing is dropped.
  if (!action.name) {
    const plain = <span className={styles.chip}>{badge}</span>;
    return tip ? <Tooltip content={tip}>{plain}</Tooltip> : plain;
  }

  const link = (
    <Link
      to="/web/notifications"
      search={{ tab: "actions", actionSearch: nameQuery(action.name) }}
      className={[styles.chip, styles.chipLink].join(" ")}
    >
      {badge}
    </Link>
  );
  return tip ? <Tooltip content={tip}>{link}</Tooltip> : link;
}

// notificationBranches groups the alert's actions under the notification that
// triggered each (action.notification). Branch order follows row.notifications;
// any notification referenced only by an action is appended. A matched
// notification with no actions still gets a branch. Actions with no/unknown
// attribution fall into a trailing headerless bucket so nothing is dropped.
function notificationBranches(
  notifications: string[],
  actions: ActionResult[],
): { name: string; actions: ActionResult[] }[] {
  // De-duplicate notification names. Names are not unique across notification
  // entries (entries are keyed by uid, not name), and a branch's actions are
  // attributed purely by name (see the filter below) — so two entries sharing
  // a name are indistinguishable here and must collapse into one branch.
  // Without this, `["oncall", "oncall"]` would render two identical branches
  // (same key, same filtered actions).
  const order: string[] = [];
  for (const n of notifications) {
    if (!order.includes(n)) order.push(n);
  }
  for (const a of actions) {
    const n = a.notification ?? "";
    if (n && !order.includes(n)) order.push(n);
  }
  const branches = order.map((name) => ({
    name,
    actions: actions.filter((a) => (a.notification ?? "") === name),
  }));
  const orphaned = actions.filter((a) => !(a.notification ?? ""));
  if (orphaned.length > 0) {
    branches.push({ name: "", actions: orphaned });
  }
  return branches;
}

function NotificationBranch({ name, actions }: { name: string; actions: ActionResult[] }) {
  return (
    <div className={styles.branch}>
      {name ? (
        <div className={styles.branchHead}>
          <Link
            to="/web/notifications"
            search={{ tab: "notifications", search: nameQuery(name) }}
            className={styles.entityLink}
          >
            {name}
          </Link>
        </div>
      ) : null}
      {actions.length > 0 ? (
        <div className={styles.branchActions}>
          {actions.map((a, i) => (
            <ActionChip key={`${a.name ?? "action"}-${i}`} action={a} />
          ))}
        </div>
      ) : (
        <span className={styles.none}>no actions</span>
      )}
    </div>
  );
}

export function AlertFlowChart({ row }: { row: Record_ }) {
  const rules = row.rules ?? [];
  const notifications = row.notifications ?? [];
  const actions = row.actions ?? [];
  const snoozed = row.snoozed;
  // "default" is the aggregaterule plugin's synthetic fallback bucket — it has
  // no backing rule, so a deep-link would dead-end on an empty list. Render it
  // (and the empty "—") as plain text; link only real aggregate rule names.
  const aggregateLinkable = row.aggregate && row.aggregate !== "default";

  return (
    // The sizing container the stage layout keys off: narrow (the drawer's
    // Flow tab on a laptop) stacks the stages top-to-bottom; wide (the alerts
    // table's inline expander, or the drawer on a NOC screen) lays the same
    // stages left-to-right, which is both the shape a pipeline wants and four
    // times shorter — it matters when the trace is hanging inside a list the
    // operator is still scanning.
    <div className={styles.flowContainer}>
      <div className={styles.flow}>
        <Node label="Input">
          <span className={styles.value}>{row.source || "—"}</span>
        </Node>
        <Connector />
        <Node label="Rules">
          {rules.length > 0 ? (
            <span className={styles.value}>
              {rules.map((r, i) => (
                <Fragment key={`${r}-${i}`}>
                  {i > 0 ? ", " : null}
                  <Link
                    to="/web/rules"
                    search={{ tab: "rules", search: nameQuery(r) }}
                    className={styles.entityLink}
                  >
                    {r}
                  </Link>
                </Fragment>
              ))}
            </span>
          ) : (
            <span className={styles.none}>none</span>
          )}
        </Node>
        <Connector />
        <Node label="Aggregate">
          <span className={styles.value}>
            {aggregateLinkable ? (
              <Link
                to="/web/rules"
                search={{ tab: "aggregates", aggSearch: nameQuery(row.aggregate as string) }}
                className={styles.entityLink}
              >
                {row.aggregate}
              </Link>
            ) : (
              row.aggregate || "—"
            )}
          </span>
          {/* hash is an extra key stamped by the aggregaterule plugin; not in the Record schema, hence the typeof guard */}
          {typeof row.hash === "string" && row.hash ? (
            <span className={styles.subtle}>{row.hash.slice(0, 12)}</span>
          ) : null}
        </Node>
        <Connector />
        {snoozed ? (
          <Node label="Snooze">
            <Link
              to="/web/snoozes"
              search={{ search: nameQuery(snoozed) }}
              className={styles.chipLink}
            >
              <Badge variant="muted">
                <span aria-hidden="true">⊘</span> {snoozed}
              </Badge>
            </Link>
            <span className={styles.subtle}>silenced — pipeline stopped</span>
          </Node>
        ) : (
          <Node label="Notifications">
            {notifications.length > 0 || actions.length > 0 ? (
              <div className={styles.fork}>
                {notificationBranches(notifications, actions).map((b, i) => (
                  <NotificationBranch
                    key={`${b.name || "orphaned"}-${i}`}
                    name={b.name}
                    actions={b.actions}
                  />
                ))}
              </div>
            ) : (
              <span className={styles.none}>none</span>
            )}
          </Node>
        )}
      </div>
    </div>
  );
}
