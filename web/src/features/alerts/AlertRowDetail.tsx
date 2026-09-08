import { JsonViewer } from "@/shared/ui/JsonViewer";
import { Tabs, TabList, TabTrigger, TabPanel } from "@/shared/ui/Tabs";
import { Badge } from "@/shared/ui/Badge";
import { TimeCell } from "@/shared/ui/TimeCell";
import { EmptyState } from "@/shared/ui/EmptyState";
import { severityColor } from "@/lib/format/severity-color";
import { DeliveryTimeline } from "@/features/notifications/deliveries/DeliveryTimeline";
import { useDeliverySummary } from "@/features/notifications/deliveries/api";
import { useCanReadDeliveries } from "@/features/notifications/deliveries/perms";
import type { DeliveryFilter } from "@/features/notifications/deliveries/types";
import { escalationLabel, severityDisplayLabel, stateBadgeVariant, stateLabel } from "./format";
import { lastDeliverySummary } from "./lastDelivery";
import { CommentTimeline } from "./CommentTimeline";
import { AlertFlowChart } from "./AlertFlowChart";
import type { AlertState, Record_ } from "./types";
import styles from "./AlertRowDetail.module.css";

export type AlertRowDetailProps = {
  row: Record_;
};

function stripPrivateKeys(row: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(row)) {
    if (k.startsWith("_")) continue;
    out[k] = v;
  }
  return out;
}

/**
 * AlertRowDetail — the body of the docked row inspector on the alerts list.
 *
 * A compact summary header (severity + state badges, an escalation badge when
 * the alert has been re-escalated, source chip, the alert message, received
 * time) sits above four tabs:
 *   - Timeline (default): comment/activity history + composer — the read-write
 *     action surface, given top billing since triage lives here.
 *   - Flow: the pipeline path the alert took (AlertFlowChart) — read-only.
 *   - Deliveries: every send this alert was part of, from the delivery log.
 *     Hidden entirely (and its queries never fired) for roles without
 *     `ro_notificationlog`, and for a row with no uid to scope on.
 *   - Record: the raw record (JsonViewer) — read-only reference.
 *
 * The host is intentionally NOT repeated here — it is the inspector's title.
 * One layout for every viewport: the panel is already narrow, so the old
 * desktop 3-column grid (and its useIsMobileShell fork) is gone. Radix Tabs
 * mounts only the active panel, so CommentTimeline is instantiated once and its
 * comment fetch fires once.
 */
export function AlertRowDetail({ row }: AlertRowDetailProps) {
  const cleaned = stripPrivateKeys(row as unknown as Record<string, unknown>);
  const state = (row.state ?? "") as AlertState;
  // Only rendered when the alert has actually been re-escalated: a
  // first-delivery alert carries no extra chrome.
  const escalation = escalationLabel(
    row.escalation_count,
    row.escalation_reason,
    row.escalation_actor,
  );

  // An alert row can legitimately have no uid (see AlertsPage's `recordKey`
  // comment — host+timestamp fallback), and an empty scope is not a narrower
  // query but a catastrophically wider one: the server evaluates CONTAINS as a
  // regex, so `alert_uids CONTAINS ""` matches every row in the tenant. The
  // hook refuses to fire without an identifier, so there is nothing to fake.
  const canReadDeliveries = useCanReadDeliveries();
  const deliveryFilter: DeliveryFilter = { kind: "alert", uid: row.uid ?? "" };
  const showDeliveries = canReadDeliveries && !!row.uid;
  // ONE limit=1, newest-first request answers both questions the header and
  // the tab label ask: `meta.total` is the count on the tab, `data[0]` is the
  // "Last notified …" line. Asking twice would be two full scans per inspector
  // open for facts that live in the same response.
  //
  // It polls on the same 30 s cadence as the Deliveries tab's rows: this
  // component only exists while the inspector is open, and a tab badge or a
  // "Last notified …" line stuck at its mount value beside a list that keeps
  // updating states two different moments in one panel.
  const deliverySummary = useDeliverySummary(deliveryFilter, {
    enabled: showDeliveries,
    live: true,
  });
  const lastDelivery = showDeliveries ? lastDeliverySummary(deliverySummary.latest) : null;
  const deliveriesTabLabel =
    deliverySummary.total !== undefined && deliverySummary.total > 0
      ? `Deliveries · ${deliverySummary.total}`
      : "Deliveries";

  return (
    <div className={styles.detail}>
      <div className={styles.summary}>
        <div className={styles.badges}>
          <Badge color={severityColor(row.severity ?? "")} title={row.severity ?? "—"}>
            {row.severity ? severityDisplayLabel(row.severity) : "—"}
          </Badge>
          <Badge variant={stateBadgeVariant(state)}>{stateLabel(state)}</Badge>
          {escalation ? <Badge variant="warning">{escalation}</Badge> : null}
          {row.source ? <span className={styles.source}>{row.source}</span> : null}
        </div>
        {row.message ? <p className={styles.message}>{row.message}</p> : null}
        <div className={styles.received}>
          <TimeCell epoch={row.date_epoch} />
        </div>
        {lastDelivery ? (
          <div
            className={
              lastDelivery.variant === "error" ? styles.lastDeliveryError : styles.lastDelivery
            }
          >
            {lastDelivery.variant === "error" ? (
              <>
                {"Last delivery failed "}
                <TimeCell epoch={lastDelivery.epoch} compact />
                {lastDelivery.via ? ` (${lastDelivery.via})` : null}
              </>
            ) : (
              <>
                {"Last notified "}
                <TimeCell epoch={lastDelivery.epoch} compact />
                {lastDelivery.via ? (
                  <>
                    {" via "}
                    <span className={styles.lastDeliveryAction}>{lastDelivery.via}</span>
                  </>
                ) : null}
              </>
            )}
            {lastDelivery.batchCount ? ` · batch of ${lastDelivery.batchCount}` : null}
          </div>
        ) : null}
      </div>

      <Tabs defaultValue="timeline">
        <TabList>
          <TabTrigger value="timeline">Timeline</TabTrigger>
          <TabTrigger value="flow">Flow</TabTrigger>
          {/* Deliveries follows Flow: both answer "what did the pipeline do
              with this alert?", and Record is the raw-JSON fallback that
              closes the strip. */}
          {showDeliveries ? <TabTrigger value="deliveries">{deliveriesTabLabel}</TabTrigger> : null}
          <TabTrigger value="record">Record</TabTrigger>
        </TabList>
        <TabPanel value="timeline">
          <CommentTimeline recordUid={row.uid} state={row.state} />
        </TabPanel>
        <TabPanel value="flow">
          <AlertFlowChart row={row} />
        </TabPanel>
        {showDeliveries ? (
          <TabPanel value="deliveries">
            <DeliveryTimeline
              filter={deliveryFilter}
              variant="alert"
              live
              emptyState={
                <EmptyState
                  icon="bell-off"
                  title="No deliveries"
                  description="No notification has sent this alert anywhere yet."
                />
              }
            />
          </TabPanel>
        ) : null}
        <TabPanel value="record">
          <JsonViewer value={cleaned} />
        </TabPanel>
      </Tabs>
    </div>
  );
}
