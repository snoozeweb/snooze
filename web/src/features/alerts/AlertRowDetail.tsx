import { useCallback, useState } from "react";
import { JsonViewer } from "@/shared/ui/JsonViewer";
import { Tabs, TabList, TabTrigger, TabPanel } from "@/shared/ui/Tabs";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
} from "@/shared/ui/Dialog";
import { TimeCell } from "@/shared/ui/TimeCell";
import { EmptyState } from "@/shared/ui/EmptyState";
import { severityColor } from "@/lib/format/severity-color";
import { DeliveryTimeline } from "@/features/notifications/deliveries/DeliveryTimeline";
import { useDeliverySummary } from "@/features/notifications/deliveries/api";
import { useCanReadDeliveries } from "@/features/notifications/deliveries/perms";
import type { DeliveryFilter } from "@/features/notifications/deliveries/types";
import { AnalysisTab } from "./analysis/AnalysisTab";
import { ConfidenceBadge } from "./analysis/ConfidenceBadge";
import { useAnalysis } from "./analysis/api";
import { confidenceLabel, isConfidence } from "./analysis/enums";
import { escalationLabel, severityDisplayLabel, stateBadgeVariant, stateLabel } from "./format";
import { lastDeliverySummary } from "./lastDelivery";
import { CommentTimeline } from "./CommentTimeline";
import { AlertFlowChart } from "./AlertFlowChart";
import type { AlertState, Record_ } from "./types";
import styles from "./AlertRowDetail.module.css";

/** Which surface the inspector opens on. */
export type AlertDetailTab = "timeline" | "flow" | "analysis" | "deliveries" | "record";

export type AlertRowDetailProps = {
  row: Record_;
  /**
   * The tab shown on open. Defaults to Timeline, where triage lives. A deep
   * link that means to land somewhere else (T3's dashboard panel points at
   * "analysis") passes it through.
   */
  defaultTab?: AlertDetailTab;
  /**
   * Told when the Analysis editor opens or closes (and `false` on unmount).
   * The host owns the affordances that would destroy it from outside —
   * prev/next on the drawer, a row click in the grid — and can only guard them
   * if it knows there is an unsaved draft in here.
   */
  onEditingChange?: ((editing: boolean) => void) | undefined;
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
 * time, the last delivery, and the analysed cause when there is one) sits
 * above five tabs:
 *   - Timeline (default): comment/activity history + composer — the read-write
 *     action surface, given top billing since triage lives here.
 *   - Flow: the pipeline path the alert took (AlertFlowChart) — read-only.
 *   - Analysis: why the alert fired and what to do about it, as the alert-rca
 *     agent loop (or an operator holding `rw_protected`) recorded it. Hidden
 *     for a row with no uid — the analysis is addressed by its record's uid.
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
export function AlertRowDetail({
  row,
  defaultTab = "timeline",
  onEditingChange,
}: AlertRowDetailProps) {
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

  // The SAME query the Analysis tab runs. TanStack Query serves both readers
  // from one request (identical key), so labelling the tab with the confidence
  // and printing the cause in the header costs no extra network. A 404 —
  // "this alert carries no analysis", the normal case — resolves to null, so
  // neither surface appears rather than showing an error.
  const showAnalysis = !!row.uid;
  const analysis = useAnalysis(row.uid, { enabled: showAnalysis });
  const rootCause = analysis.data?.agentic.root_cause;
  // `confidence` is whatever the stored document carries — a hand-written
  // analysis, or one from a server that knows a level this bundle does not.
  // Read unguarded it prints "Analysis · undefined" on the tab.
  const confidence = isConfidence(rootCause?.confidence) ? rootCause.confidence : undefined;
  const analysisTabLabel =
    confidence !== undefined ? `Analysis · ${confidenceLabel(confidence)}` : "Analysis";

  // Which surface is on screen. Controlled rather than uncontrolled because
  // two things have to be arbitrated here and Radix cannot do either on its
  // own: a value whose trigger does not exist (a deep link to Analysis on a
  // uid-less row) selects NOTHING — five tab stops and no panel — and a tab
  // switch silently unmounts an open editor with an unsaved draft in it.
  const [tab, setTab] = useState<AlertDetailTab>(defaultTab);
  const [editing, setEditing] = useState(false);
  const [pendingTab, setPendingTab] = useState<AlertDetailTab | null>(null);
  const hasTrigger = (t: AlertDetailTab) =>
    t === "analysis" ? showAnalysis : t === "deliveries" ? showDeliveries : true;
  const activeTab: AlertDetailTab = hasTrigger(tab) ? tab : "timeline";

  // AnalysisTab reports its own mode; the effect that does it runs on every
  // change, so the callback it depends on has to keep one identity.
  const handleEditingChange = useCallback(
    (next: boolean) => {
      setEditing(next);
      onEditingChange?.(next);
    },
    [onEditingChange],
  );

  const handleTabChange = (next: string) => {
    const wanted = next as AlertDetailTab;
    // Only the analysis editor is guarded: the Timeline composer keeps its
    // text in the query cache and is restored on return, so a tab switch
    // there costs nothing and must not grow a prompt.
    if (editing && activeTab === "analysis" && wanted !== "analysis") {
      setPendingTab(wanted);
      return;
    }
    setTab(wanted);
  };

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
        {rootCause ? (
          <div className={styles.cause}>
            <span className={styles.causeLabel}>Cause:</span>
            <span className={styles.causeText}>{rootCause.summary}</span>
            {confidence !== undefined ? <ConfidenceBadge confidence={confidence} /> : null}
          </div>
        ) : null}
      </div>

      <Tabs value={activeTab} onValueChange={handleTabChange}>
        <TabList>
          <TabTrigger value="timeline">Timeline</TabTrigger>
          <TabTrigger value="flow">Flow</TabTrigger>
          {/* Analysis sits between Flow and Deliveries: Flow says what the
              pipeline did, Analysis says why the alert fired at all. */}
          {showAnalysis ? <TabTrigger value="analysis">{analysisTabLabel}</TabTrigger> : null}
          {/* Deliveries follows: both it and Flow answer "what did the pipeline
              do with this alert?", and Record is the raw-JSON fallback that
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
        {/* Mounted whatever the row is: AnalysisTab handles a missing uid on
            its own, and a panel that exists for a value that can be selected
            is what keeps the inspector from rendering nothing at all. Only the
            trigger is conditional. */}
        <TabPanel value="analysis">
          <AnalysisTab uid={row.uid} onEditingChange={handleEditingChange} />
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

      <DiscardAnalysisDraftDialog
        open={pendingTab !== null}
        onOpenChange={(open) => {
          if (!open) setPendingTab(null);
        }}
        onDiscard={() => {
          if (pendingTab !== null) setTab(pendingTab);
          setPendingTab(null);
        }}
        description="Leaving the Analysis tab closes the editor. Anything you have written here is not saved."
      />
    </div>
  );
}

export type DiscardAnalysisDraftDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Run when the operator accepts losing the draft. */
  onDiscard: () => void;
  /** What exactly is about to destroy it — the caller knows, this does not. */
  description: string;
};

/**
 * The prompt shown before something outside the analysis editor destroys it:
 * a tab switch inside the inspector, or a prev/next retarget of the whole
 * drawer. Both are one click away from an open editor, and the editor's own
 * Cancel is the only other way out — so the question is asked once, in one
 * voice, from here.
 *
 * In-DOM and never `window.confirm`: Playwright auto-dismisses the native one,
 * which would make the guard silently untestable (web/AGENTS.md).
 */
export function DiscardAnalysisDraftDialog({
  open,
  onOpenChange,
  onDiscard,
  description,
}: DiscardAnalysisDraftDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogTitle>Discard this analysis draft?</DialogTitle>
        <DialogBody>
          <DialogDescription>{description}</DialogDescription>
        </DialogBody>
        <DialogFooter>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            Keep editing
          </Button>
          <Button variant="danger" onClick={onDiscard}>
            Discard draft
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
