// One dispatch: what fired, when, which actions carried it, whether each of
// them landed, and which alerts were in it. An <li> on the timeline's <ol>,
// with the rail dot carrying the outcome of the whole group.
//
// The backend logs one row per (alert × action); `groupDeliveries` folds a
// notification's fan-out back into a single visual row, so the actions read as
// chips side by side — green when the send landed, red when it did not —
// instead of as N near-identical rows a second apart. Every column here is a
// grid track shared with the sibling rows (see the subgrid in the CSS), so
// timestamps, routes and action chips line up down the whole list.
import { useMemo, useState } from "react";
import { Link } from "@tanstack/react-router";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { IconButton } from "@/shared/ui/IconButton";
import { TimeCell } from "@/shared/ui/TimeCell";
import { Icon } from "@/shared/icons/Icon";
import { toast } from "@/shared/ui/toast/useToast";
import { DeliveryAlertLine } from "./DeliveryAlertLine";
import {
  deliveryActionName,
  deliveryChipTitle,
  deliveryGroupAriaLabel,
  deliveryStatusLabel,
  sortAlertsBySeverity,
} from "./format";
import { alertsForDelivery } from "./links";
import type { DeliveryGroup } from "./group";
import type { DeliveryEntry, DeliveryVariant } from "./types";
import styles from "./DeliveryRow.module.css";

/** Alert lines shown before the "+N more" toggle takes over. */
export const ALERT_PREVIEW_COUNT = 5;

/**
 * Error text longer than this gets a Show more toggle. A character budget
 * rather than a measured line count: the clamp itself is CSS (3 lines), and
 * jsdom cannot measure, so the toggle has to be decided from the string.
 */
const ERROR_CLAMP_CHARS = 160;

export type DeliveryRowProps = {
  group: DeliveryGroup;
  variant: DeliveryVariant;
  /**
   * Whether the list reserves a column for the routing notifications. Set by
   * the timeline for the whole list, not per row: an empty track on some rows
   * and a filled one on others would be exactly the misalignment the grid is
   * here to remove.
   */
  showSubject: boolean;
};

export function DeliveryRow({ group, variant, showSubject }: DeliveryRowProps) {
  const [alertsExpanded, setAlertsExpanded] = useState(false);

  const failed = group.failed > 0;
  const alerts = useMemo(() => sortAlertsBySeverity(group.alerts), [group.alerts]);
  const showAlertLines = variant !== "alert" && alerts.length > 0;
  const visibleAlerts = alertsExpanded ? alerts : alerts.slice(0, ALERT_PREVIEW_COUNT);

  const failures = group.rows.filter((r) => r.status === "error");
  const refs = group.rows.filter((r) => typeof r.ref?.url === "string");
  const multi = group.rows.length > 1;
  const hasExtras =
    showAlertLines || group.alertCount > 1 || failures.length > 0 || refs.length > 0;

  return (
    <li className={styles.row} aria-label={deliveryGroupAriaLabel(group)}>
      <span className={styles.dot} data-status={failed ? "error" : "success"} aria-hidden="true" />

      <div className={styles.time}>
        <TimeCell epoch={group.dateEpoch} />
      </div>

      {showSubject ? (
        <div className={styles.subject}>
          {group.notifications.map((name) => (
            <Badge key={name} variant="neutral" title={name}>
              {name}
            </Badge>
          ))}
        </div>
      ) : null}

      <div className={styles.actions}>
        {group.rows.map((row, i) => (
          <ActionChip
            key={row.uid ?? `${deliveryActionName(row)}-${row.date_epoch ?? 0}-${i}`}
            row={row}
            variant={variant}
          />
        ))}
      </div>

      <div className={styles.meta}>
        {group.batch ? <Badge variant="info">{group.batch}</Badge> : null}
        {group.escalation ? <Badge variant="warning">{group.escalation}</Badge> : null}
      </div>

      {hasExtras ? (
        <div className={styles.extras}>
          {showAlertLines ? (
            <ul className={styles.alerts}>
              {visibleAlerts.map((alert, i) => (
                <DeliveryAlertLine key={alert.uid || alert.hash || `${i}`} alert={alert} />
              ))}
            </ul>
          ) : null}

          {showAlertLines && alerts.length > ALERT_PREVIEW_COUNT ? (
            <Button
              size="sm"
              variant="ghost"
              className={styles.moreButton}
              aria-expanded={alertsExpanded}
              onClick={() => setAlertsExpanded((v) => !v)}
            >
              {alertsExpanded ? "Show fewer" : `+${alerts.length - ALERT_PREVIEW_COUNT} more`}
            </Button>
          ) : null}

          {group.alertCount > 1 && group.rows[0] ? (
            <Link {...alertsForDelivery(group.rows[0])} className={styles.viewAll}>
              {`View all ${group.alertCount} alerts →`}
            </Link>
          ) : null}

          {failures.map((row, i) => (
            <DeliveryError
              key={row.uid ?? `err-${i}`}
              row={row}
              // Name the action only when the row carries several: on a
              // single-action dispatch the chip above already said it.
              showAction={multi}
            />
          ))}

          {refs.map((row, i) => {
            const url = row.ref?.url;
            if (!url) return null;
            const key = typeof row.ref?.["issue_key"] === "string" ? row.ref["issue_key"] : "";
            const via = multi ? `${deliveryActionName(row)}: ` : "";
            return (
              <a
                key={row.uid ?? `ref-${i}`}
                className={styles.refLink}
                href={url}
                target="_blank"
                rel="noreferrer"
              >
                {`${via}Open ${key || "reference"} ↗`}
              </a>
            );
          })}
        </div>
      ) : null}
    </li>
  );
}

/**
 * One action's outcome inside the row. Colour says sent/failed at a glance;
 * the icon repeats it in shape and the visually-hidden word repeats it in
 * text, so the signal survives both colour blindness and a screen reader.
 * On the action inspector the action name is the scope of the whole tab, so
 * the chip spends its width on the status word instead of repeating it.
 */
function ActionChip({ row, variant }: { row: DeliveryEntry; variant: DeliveryVariant }) {
  const failed = row.status === "error";
  const status = deliveryStatusLabel(row.status);
  const name = deliveryActionName(row);
  const label = variant === "action" || !name ? status : name;

  return (
    <Badge
      variant={failed ? "critical" : "ok"}
      className={styles.chip ?? ""}
      title={deliveryChipTitle(row)}
    >
      <Icon name={failed ? "alert-triangle" : "check"} size={12} />
      <span className={styles.chipLabel}>{label}</span>
      {label === status ? null : <span className={styles.srOnly}>{status}</span>}
    </Badge>
  );
}

/** The failure text of one member send: clamped, expandable, copyable. */
function DeliveryError({ row, showAction }: { row: DeliveryEntry; showAction: boolean }) {
  const [expanded, setExpanded] = useState(false);
  const text = row.error ?? "";
  if (!text) return null;
  const long = text.length > ERROR_CLAMP_CHARS || text.includes("\n");
  const name = deliveryActionName(row);

  async function copyError() {
    try {
      await navigator.clipboard.writeText(text);
      toast.success("Error copied to clipboard");
    } catch {
      toast.error("Copy failed — select and copy manually");
    }
  }

  return (
    // `data-long` is what lets the narrow layout stack the buttons under the
    // text only when the text actually needs the width — a one-line
    // "script: exit 1" keeps its copy button inline.
    <div className={styles.error} data-long={long || undefined}>
      <pre className={styles.errorText} data-clamped={!expanded && long ? "true" : undefined}>
        {showAction && name ? <span className={styles.errorAction}>{`${name}: `}</span> : null}
        {text}
      </pre>
      <div className={styles.errorActions}>
        {long ? (
          <Button
            size="sm"
            variant="ghost"
            aria-expanded={expanded}
            onClick={() => setExpanded((v) => !v)}
          >
            {expanded ? "Show less" : "Show more"}
          </Button>
        ) : null}
        <IconButton
          icon="copy"
          label={showAction && name ? `Copy error from ${name}` : "Copy error"}
          size="sm"
          variant="ghost"
          onClick={() => void copyError()}
        />
      </div>
    </div>
  );
}
