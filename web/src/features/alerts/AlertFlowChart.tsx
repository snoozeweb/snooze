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
        <>
          <Node label="Notifications">
            {notifications.length > 0 ? (
              <span className={styles.value}>{notifications.join(", ")}</span>
            ) : (
              <span className={styles.none}>none</span>
            )}
          </Node>
          <Connector />
          <Node label="Actions">
            {actions.length > 0 ? (
              <div className={styles.chips}>
                {actions.map((a, i) => (
                  <ActionChip key={`${a.name ?? "action"}-${i}`} action={a} />
                ))}
              </div>
            ) : (
              <span className={styles.none}>none</span>
            )}
          </Node>
        </>
      )}
    </div>
  );
}
