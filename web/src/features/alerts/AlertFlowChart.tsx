// AlertFlowChart — the pipeline path a single alert actually took:
//   input → rules → aggregate → (snooze, terminal) | notifications → actions
// All data comes from the record row; no fetch. Colours via Badge variants only.
import type { ReactNode } from "react";
import { Badge, type BadgeVariant } from "@/shared/ui/Badge";
import { Popover, PopoverTrigger, PopoverContent } from "@/shared/ui/Popover";
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
  if (status === "error" && action.error) {
    return (
      <Popover>
        <PopoverTrigger
          type="button"
          className={styles.chipButton}
          aria-label={`${action.name ?? "Action"} error details`}
        >
          {badge}
        </PopoverTrigger>
        <PopoverContent>
          <div className={styles.errorMsg}>{action.error}</div>
        </PopoverContent>
      </Popover>
    );
  }
  const hint = ACTION_HINT[status];
  if (hint) {
    return (
      <Tooltip content={hint}>
        <span className={styles.chip}>{badge}</span>
      </Tooltip>
    );
  }
  return <span className={styles.chip}>{badge}</span>;
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
  const order: string[] = [...notifications];
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
      {name ? <div className={styles.branchHead}>{name}</div> : null}
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

  return (
    <div className={styles.flow}>
      <Node label="Input">
        <span className={styles.value}>{row.source || "—"}</span>
      </Node>
      <Connector />
      <Node label="Rules">
        {rules.length > 0 ? (
          <span className={styles.value}>{rules.join(", ")}</span>
        ) : (
          <span className={styles.none}>none</span>
        )}
      </Node>
      <Connector />
      <Node label="Aggregate">
        <span className={styles.value}>{row.aggregate || "—"}</span>
        {/* hash is an extra key stamped by the aggregaterule plugin; not in the Record schema, hence the typeof guard */}
        {typeof row.hash === "string" && row.hash ? (
          <span className={styles.subtle}>{row.hash.slice(0, 12)}</span>
        ) : null}
      </Node>
      <Connector />
      {snoozed ? (
        <Node label="Snooze">
          <Badge variant="muted">
            <span aria-hidden="true">⊘</span> {snoozed}
          </Badge>
          <span className={styles.subtle}>silenced — pipeline stopped</span>
        </Node>
      ) : (
        <Node label="Notifications">
          {notifications.length > 0 || actions.length > 0 ? (
            <div className={styles.fork}>
              {notificationBranches(notifications, actions).map((b, i) => (
                <NotificationBranch key={b.name || `orphaned-${i}`} name={b.name} actions={b.actions} />
              ))}
            </div>
          ) : (
            <span className={styles.none}>none</span>
          )}
        </Node>
      )}
    </div>
  );
}
