// One delivery: what fired, when, whether it landed, and which alerts were in
// it. An <li> on the timeline's <ol>, with the rail dot carrying the outcome.
import { useMemo, useState } from "react";
import { Link } from "@tanstack/react-router";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { IconButton } from "@/shared/ui/IconButton";
import { TimeCell } from "@/shared/ui/TimeCell";
import { toast } from "@/shared/ui/toast/useToast";
import { DeliveryAlertLine } from "./DeliveryAlertLine";
import {
  batchLabel,
  deliveryAlertCount,
  deliveryAriaLabel,
  deliveryStatusLabel,
  escalationBadge,
  sortAlertsBySeverity,
} from "./format";
import { alertsForDelivery } from "./links";
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
  row: DeliveryEntry;
  variant: DeliveryVariant;
};

export function DeliveryRow({ row, variant }: DeliveryRowProps) {
  const [alertsExpanded, setAlertsExpanded] = useState(false);
  const [errorExpanded, setErrorExpanded] = useState(false);

  const failed = row.status === "error";
  const alerts = useMemo(() => sortAlertsBySeverity(row.alerts ?? []), [row.alerts]);
  const showAlertLines = variant !== "alert" && alerts.length > 0;
  const visibleAlerts = alertsExpanded ? alerts : alerts.slice(0, ALERT_PREVIEW_COUNT);

  const totalAlerts = deliveryAlertCount(row);
  const batch = batchLabel(row);
  const escalation = escalationBadge(row);
  const notifications = row.notification_names ?? [];
  const errorText = row.error ?? "";
  const errorLong = errorText.length > ERROR_CLAMP_CHARS || errorText.includes("\n");
  const refUrl = row.ref?.url;
  const refKey = typeof row.ref?.["issue_key"] === "string" ? row.ref["issue_key"] : undefined;

  async function copyError() {
    try {
      await navigator.clipboard.writeText(errorText);
      toast.success("Error copied to clipboard");
    } catch {
      toast.error("Copy failed — select and copy manually");
    }
  }

  return (
    <li className={styles.row} aria-label={deliveryAriaLabel(row)}>
      <span className={styles.dot} data-status={failed ? "error" : "success"} aria-hidden="true" />
      <div className={styles.body}>
        <div className={styles.headline}>
          <TimeCell epoch={row.date_epoch} />
          {variant !== "action" && row.action ? (
            <Badge variant="info" title={row.action}>
              {row.action}
            </Badge>
          ) : null}
          {variant !== "notification"
            ? notifications.map((name) => (
                <Badge key={name} variant="neutral" title={name}>
                  {name}
                </Badge>
              ))
            : null}
          {row.notifier ? (
            <span className={styles.notifier} title={`Notifier: ${row.notifier}`}>
              {row.notifier}
            </span>
          ) : null}
          <Badge variant={failed ? "critical" : "ok"}>{deliveryStatusLabel(row.status)}</Badge>
          {batch ? <Badge variant="info">{batch}</Badge> : null}
          {escalation ? <Badge variant="warning">{escalation}</Badge> : null}
        </div>

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

        {totalAlerts > 1 ? (
          <Link {...alertsForDelivery(row)} className={styles.viewAll}>
            {`View all ${totalAlerts} alerts →`}
          </Link>
        ) : null}

        {failed && errorText ? (
          // `data-long` is what lets the narrow layout stack the buttons under
          // the text only when the text actually needs the width — a one-line
          // "script: exit 1" keeps its copy button inline.
          <div className={styles.error} data-long={errorLong || undefined}>
            <pre
              className={styles.errorText}
              data-clamped={!errorExpanded && errorLong ? "true" : undefined}
            >
              {errorText}
            </pre>
            <div className={styles.errorActions}>
              {errorLong ? (
                <Button
                  size="sm"
                  variant="ghost"
                  aria-expanded={errorExpanded}
                  onClick={() => setErrorExpanded((v) => !v)}
                >
                  {errorExpanded ? "Show less" : "Show more"}
                </Button>
              ) : null}
              <IconButton
                icon="copy"
                label="Copy error"
                size="sm"
                variant="ghost"
                onClick={() => void copyError()}
              />
            </div>
          </div>
        ) : null}

        {refUrl ? (
          <a className={styles.refLink} href={refUrl} target="_blank" rel="noreferrer">
            {`Open ${refKey ?? "reference"} ↗`}
          </a>
        ) : null}
      </div>
    </li>
  );
}
