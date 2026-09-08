// One alert covered by a delivery: severity, host, message — the whole line
// being a link into the alerts inspector. Rendered under a delivery row on
// the notification and action inspectors; suppressed on the alert inspector,
// where the row IS the alert.
import { Link } from "@tanstack/react-router";
import { Badge } from "@/shared/ui/Badge";
import { severityDisplayLabel } from "@/features/alerts/format";
import { severityColor } from "@/lib/format/severity-color";
import { alertRecordLink } from "./links";
import type { DeliveryAlert } from "./types";
import styles from "./DeliveryAlertLine.module.css";

export type DeliveryAlertLineProps = {
  alert: DeliveryAlert;
};

export function DeliveryAlertLine({ alert }: DeliveryAlertLineProps) {
  const link = alertRecordLink(alert);
  const severity = (alert.severity ?? "").trim();
  const host = alert.host ?? "";
  const message = alert.message ?? "";
  // The link's accessible name — the visible text is three fragments that read
  // as a sentence only with the layout, and the message is ellipsised.
  const label = [severity && severityDisplayLabel(severity), host, message]
    .filter(Boolean)
    .join(" — ");

  return (
    <li className={styles.line}>
      <Link {...link} className={styles.link} aria-label={`Open alert: ${label}`}>
        {severity ? (
          <Badge color={severityColor(severity)} title={severity}>
            {severityDisplayLabel(severity)}
          </Badge>
        ) : null}
        {host ? (
          <span className={styles.host} title={host}>
            {host}
          </span>
        ) : null}
        <span className={styles.message} title={message}>
          {message}
        </span>
      </Link>
    </li>
  );
}
