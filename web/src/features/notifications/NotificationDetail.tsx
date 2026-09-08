// The notifications page's row inspector.
//
// Replaces the bare `RowDetailPanel` with a summary strip (what this
// notification IS) over three tabs (what it DID / its raw record / who
// changed it). Deliveries leads, because "did anyone actually get paged?" is
// the question that brings an operator here — the JSON is the fallback, not
// the headline.
//
// Terminology (plan Finding 15): a *notification* routes; one send is a
// **delivery**. Never call a delivery "a notification" here.
import { useState, type ReactNode } from "react";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { Code } from "@/shared/ui/Code";
import { EmptyState } from "@/shared/ui/EmptyState";
import { TabList, TabPanel, TabTrigger, Tabs } from "@/shared/ui/Tabs";
import { TimeCell } from "@/shared/ui/TimeCell";
import { AuditSection, RecordSection } from "@/shared/ui/RowDetailPanel";
import { summarizeFrequency } from "@/shared/ui/frequencyUtils";
import { prettyCondition } from "@/lib/condition/pretty";
import { DeliveryTimeline } from "./deliveries/DeliveryTimeline";
import { useCanReadDeliveries } from "./deliveries/perms";
import type { DeliveryFilter, DeliveryRange, DeliveryVariant } from "./deliveries/types";
import type { Notification } from "./types";
import styles from "./NotificationDetail.module.css";

const TAB_DELIVERIES = "deliveries";
const TAB_RECORD = "record";
const TAB_AUDIT = "audit";

export type DetailShellProps = {
  /** The identity/config strip above the tabs. */
  summary: ReactNode;
  /**
   * Delivery-log scope. Omit for an unsaved row (no uid) — the shell then
   * drops the tabs entirely and shows the raw record, which is all there is
   * to show.
   */
  filter?: DeliveryFilter | undefined;
  variant: DeliveryVariant;
  /** Copy for "this scope has never delivered anything". */
  emptyState: ReactNode;
  /** Window carried in from a dashboard deep link. */
  initialRange?: DeliveryRange | undefined;
  onRangeClear?: (() => void) | undefined;
  /** Raw record for the Record tab and the audit-log lookup. */
  row: Record<string, unknown>;
  objectType: string;
  objectId: string | undefined;
};

/**
 * DetailShell is the summary-strip + tab frame shared by the notification and
 * action inspectors. Exported (rather than copied) so the two stay visually
 * identical: same tab order, same defaults, same Record/Audit sections as
 * every other page's `RowDetailPanel`.
 */
export function DetailShell({
  summary,
  filter,
  variant,
  emptyState,
  initialRange,
  onRangeClear,
  row,
  objectType,
  objectId,
}: DetailShellProps) {
  // A role with `ro_notification` but not `ro_notificationlog` can read the
  // object and not its history. Hiding the tab (plan Risk 6) rather than
  // letting it 403 also means Record — not Deliveries — is where the drawer
  // opens for them, so the inspector still lands on something they can read.
  const canReadDeliveries = useCanReadDeliveries();
  const showDeliveries = filter !== undefined && canReadDeliveries;

  // Uncontrolled would be enough for the default, but Radix unmounts inactive
  // panels — holding the value lets the Deliveries query keep polling only
  // while its tab is the one on screen, which is exactly what we want.
  const [tab, setTab] = useState<string>(showDeliveries ? TAB_DELIVERIES : TAB_RECORD);

  if (!filter) {
    return (
      <div className={styles.panel}>
        {summary}
        <RecordSection row={row} />
      </div>
    );
  }

  return (
    <div className={styles.panel}>
      {summary}
      <Tabs value={tab} onValueChange={setTab}>
        <TabList>
          {showDeliveries ? <TabTrigger value={TAB_DELIVERIES}>Deliveries</TabTrigger> : null}
          <TabTrigger value={TAB_RECORD}>Record</TabTrigger>
          <TabTrigger value={TAB_AUDIT}>Audit log</TabTrigger>
        </TabList>
        {showDeliveries ? (
          <TabPanel value={TAB_DELIVERIES}>
            <DeliveryTimeline
              filter={filter}
              variant={variant}
              emptyState={emptyState}
              live
              {...(initialRange ? { initialRange } : {})}
              {...(onRangeClear ? { onRangeClear } : {})}
            />
          </TabPanel>
        ) : null}
        <TabPanel value={TAB_RECORD}>
          <RecordSection row={row} />
        </TabPanel>
        <TabPanel value={TAB_AUDIT}>
          <AuditSection objectType={objectType} objectId={objectId} />
        </TabPanel>
      </Tabs>
    </div>
  );
}

/**
 * The counters line: "Sent 142× · last Today 14:32", or "Never sent".
 * `hits`/`last_sent` are server-stamped and only move on a *successful*
 * delivery (D10), so "Never sent" is a real statement, not "no data".
 */
export function DeliveryCounters({ hits, lastSent }: { hits?: number; lastSent?: number }) {
  if (!hits) {
    return <p className={styles.counters}>Never sent</p>;
  }
  return (
    <p className={styles.counters}>
      <span className={styles.countersValue}>Sent {hits}×</span>
      {lastSent ? (
        <>
          <span aria-hidden="true"> · </span>
          <span className={styles.countersLast}>last</span>
          <TimeCell epoch={lastSent} />
        </>
      ) : null}
    </p>
  );
}

export type NotificationDetailProps = {
  row: Notification;
  /** Opens the editor for this notification (the page writes `?uid=`). */
  onEdit?: (uid: string) => void;
  initialRange?: DeliveryRange | undefined;
  onRangeClear?: (() => void) | undefined;
};

export function NotificationDetail({
  row,
  onEdit,
  initialRange,
  onRangeClear,
}: NotificationDetailProps) {
  const uid = row.uid;
  const actions = row.actions ?? [];
  const disabled = row.enabled === false;

  const summary = (
    <div className={styles.summary}>
      <div className={styles.identity}>
        <Code>{row.name}</Code>
        <Badge variant={disabled ? "muted" : "ok"}>{disabled ? "Disabled" : "Enabled"}</Badge>
        {actions.map((a) => (
          <Badge key={a} variant="info">
            {a}
          </Badge>
        ))}
        {actions.length === 0 ? <Badge variant="warning">No actions</Badge> : null}
      </div>
      <p className={styles.condition}>{prettyCondition(row.condition)}</p>
      <p className={styles.meta}>
        <span className={styles.metaLabel}>Frequency</span>
        <span>{summarizeFrequency(row.frequency)}</span>
      </p>
      <DeliveryCounters
        {...(row.hits !== undefined ? { hits: row.hits } : {})}
        {...(row.last_sent !== undefined ? { lastSent: row.last_sent } : {})}
      />
    </div>
  );

  // Why nothing is here matters more than the fact that nothing is here: an
  // action-less or disabled notification will NEVER deliver, and saying so
  // turns a dead end into the next step.
  const emptyState =
    actions.length === 0 ? (
      <EmptyState
        icon="bell-off"
        title="Nothing to send"
        description="This notification has no actions, so nothing is ever sent."
        action={
          uid && onEdit ? (
            <Button size="sm" variant="primary" leadingIcon="edit" onClick={() => onEdit(uid)}>
              Add an action
            </Button>
          ) : undefined
        }
      />
    ) : disabled ? (
      <EmptyState
        icon="bell-off"
        title="No deliveries yet"
        description="Disabled — deliveries resume when it is enabled."
      />
    ) : (
      <EmptyState
        icon="bell"
        title="No deliveries yet"
        description="Rows appear here the first time an alert matches."
      />
    );

  return (
    <DetailShell
      summary={summary}
      {...(uid ? { filter: { kind: "notification", uid } as DeliveryFilter } : {})}
      variant="notification"
      emptyState={emptyState}
      {...(initialRange ? { initialRange } : {})}
      {...(onRangeClear ? { onRangeClear } : {})}
      row={row as unknown as Record<string, unknown>}
      objectType="notification"
      objectId={uid}
    />
  );
}
