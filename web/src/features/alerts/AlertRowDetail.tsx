import { useCallback, useLayoutEffect, useRef, useState } from "react";
import { Link } from "@tanstack/react-router";
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
import { Icon } from "@/shared/icons/Icon";
import { AnalysisTab } from "./analysis/AnalysisTab";
import { VerdictChip } from "./analysis/VerdictChip";
import { useAnalysis } from "./analysis/api";
import { authorOf, isPlanStatus, planStatusLabel, splitSummary } from "./analysis/verdict";
import {
  escalationLabel,
  severityDisplayLabel,
  stateBadgeVariant,
  stateDeadline,
  stateLabel,
} from "./format";
import { formatAbsoluteTime, formatRelativeTime, trimDate } from "@/lib/format/time";
import { lastDeliverySummary } from "./lastDelivery";
import { CommentTimeline } from "./CommentTimeline";
import { OwnerFact } from "./Owner";
import { recordHits } from "./columns";
import { nameQuery } from "./nameQuery";
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
   * The tab on screen, when the host owns it (the alerts page keeps it in the
   * URL as `?pane=`, so Back walks the tabs and a link reopens on the same
   * one). Leave it unset for an inspector that keeps its own tab.
   */
  tab?: AlertDetailTab | undefined;
  /**
   * Asked for a tab switch once any unsaved analysis draft has been dealt
   * with. Required for `tab` to change at all.
   */
  onTabChange?: ((tab: AlertDetailTab) => void) | undefined;
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
 * The alert message, clamped to three lines. The Show more toggle appears only
 * when the clamp actually hides something — measured, since a character
 * budget offered "Show more" under messages that already fit — and stays once
 * expanded so the operator can fold it back.
 */
function AlertMessage({ text }: { text: string }) {
  const [expanded, setExpanded] = useState(false);
  const [overflows, setOverflows] = useState(false);
  const ref = useRef<HTMLParagraphElement>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || expanded) return;
    const measure = () => setOverflows(el.scrollHeight > el.clientHeight + 1);
    measure();
    // The drawer resizes (and fonts load) without the text changing.
    const ro = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(measure);
    ro?.observe(el);
    return () => ro?.disconnect();
  }, [text, expanded]);
  return (
    <div className={styles.messageBlock}>
      <p ref={ref} className={styles.message} data-clamped={expanded ? undefined : "true"}>
        {text}
      </p>
      {overflows || expanded ? (
        <button
          type="button"
          className={styles.messageToggle}
          aria-expanded={expanded}
          onClick={() => setExpanded((v) => !v)}
        >
          {expanded ? "Show less" : "Show more"}
        </button>
      ) : null}
    </div>
  );
}

/** "9m ago" (or "just now") — the compact TimeCell's words, as plain text. */
function relativeAgo(epoch: number): string {
  const rel = formatRelativeTime(epoch);
  return rel === "just now" ? rel : `${rel} ago`;
}

/** Labels shown before the "+N more" toggle. */
const LABEL_PREVIEW = 6;

/** Label keys that say where an alert lives, shown first in this order. */
const LEADING_LABELS = ["alertname", "cluster", "namespace", "job", "service", "instance", "pod"];

function labelRank(key: string): number {
  const i = LEADING_LABELS.indexOf(key);
  return i === -1 ? LEADING_LABELS.length : i;
}

/**
 * The alert's `labels` as `key=value` pairs — where-it-lives keys first (see
 * LEADING_LABELS), the rest alphabetically — with `severity` left out, as it
 * is already the first badge. Tolerates a payload whose labels are not a flat
 * string map (values are stringified).
 */
function alertLabels(row: Record_): [string, string][] {
  const raw = (row as { labels?: unknown }).labels;
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return [];
  return Object.entries(raw as Record<string, unknown>)
    .filter(([k, v]) => k !== "severity" && v !== null && v !== undefined && v !== "")
    .map(([k, v]): [string, string] => [k, typeof v === "string" ? v : JSON.stringify(v)])
    .sort(([a], [b]) => labelRank(a) - labelRank(b) || a.localeCompare(b));
}

function LabelList({ labels }: { labels: [string, string][] }) {
  const [all, setAll] = useState(false);
  const shown = all ? labels : labels.slice(0, LABEL_PREVIEW);
  return (
    <>
      <ul className={styles.labels}>
        {shown.map(([k, v]) => (
          <li key={k} className={styles.label} title={`${k}=${v}`}>
            <span className={styles.labelKey}>{k}</span>={v}
          </li>
        ))}
      </ul>
      {labels.length > LABEL_PREVIEW ? (
        <button
          type="button"
          className={styles.messageToggle}
          aria-expanded={all}
          onClick={() => setAll((v) => !v)}
        >
          {all ? "Show fewer" : `+${labels.length - LABEL_PREVIEW} more`}
        </button>
      ) : null}
    </>
  );
}

/** An http(s) link from an alert payload, or "" — never a javascript: URL. */
function safeHttpUrl(v: unknown): string {
  if (typeof v !== "string" || v === "") return "";
  try {
    const u = new URL(v);
    return u.protocol === "http:" || u.protocol === "https:" ? u.href : "";
  } catch {
    return "";
  }
}

/**
 * AlertRowDetail — the body of the docked row inspector on the alerts list.
 *
 * A compact summary header — the message, then severity/state (and an
 * escalation badge once re-escalated), then one aligned list of facts: last
 * seen and hit count, owner (or, faded, the previous one), the last delivery,
 * the snooze that silenced it, and where it came from (with a link back to
 * the source when the payload carries one); and, when the alert has been
 * analysed, one line pointing at the analysis — sits above five tabs:
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
  tab: controlledTab,
  onTabChange,
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

  const hits = recordHits(row);
  const deadline = stateDeadline(row as Parameters<typeof stateDeadline>[0]);
  // Shown only when it says something `date_epoch` does not: a first
  // occurrence that is also the last is just "when".
  const firstSeen = typeof row.first_seen === "number" ? row.first_seen : 0;
  const showFirstSeen = firstSeen > 0 && firstSeen !== row.date_epoch;
  const labels = alertLabels(row);
  // `snoozed` names the filter that silenced the alert — but it is also kept
  // on the recovery of a silenced alert, which the snooze plugin lets through
  // to notification. Only a run that stopped before notification was
  // silenced; the Flow tab draws the same distinction.
  const silencedBy =
    row.snoozed && !(row.plugins ?? []).includes("notification") ? row.snoozed : "";
  const sourceUrl = safeHttpUrl((row as { generatorURL?: unknown }).generatorURL);

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
  const notifiedText = lastDelivery
    ? [
        `${lastDelivery.variant === "error" ? "Failed " : "Notified "}${relativeAgo(lastDelivery.epoch)}`,
        lastDelivery.via ? `via ${lastDelivery.via}` : "",
      ]
        .filter(Boolean)
        .join(" ")
    : "";
  const deliveriesTabLabel =
    deliverySummary.total !== undefined && deliverySummary.total > 0
      ? `Deliveries · ${deliverySummary.total}`
      : "Deliveries";

  // The SAME query the Analysis tab runs. TanStack Query serves both readers
  // from one request (identical key), so marking the tab and pointing at the
  // analysis from the header costs no extra network. A 404 — "this alert
  // carries no analysis", the normal case — resolves to null, so neither
  // surface appears rather than showing an error.
  const showAnalysis = !!row.uid;
  const analysis = useAnalysis(row.uid, { enabled: showAnalysis });
  const agentic = analysis.data?.agentic;
  const rootCause = agentic?.root_cause;
  // The header says THAT there is an analysis and what it concludes, in one
  // line, and hands the reader to the tab for the rest. It used to print the
  // whole cause as a "Cause:" paragraph plus a confidence chip — the same
  // sentence the tab opens on, and confidence a third time (header, tab label,
  // tab body). Confidence is now said once, in the tab.
  const headline =
    rootCause && typeof rootCause.summary === "string"
      ? splitSummary(rootCause.summary, rootCause.detail).headline
      : "";
  const planStatus = agentic?.remediation_plan?.status;
  const verdict = isPlanStatus(planStatus) ? planStatus : undefined;
  const analysisNoun = authorOf(agentic?.analysis).kind === "agent" ? "AI analysis" : "Analysis";

  // Which surface is on screen. Controlled rather than uncontrolled because
  // two things have to be arbitrated here and Radix cannot do either on its
  // own: a value whose trigger does not exist (a deep link to Analysis on a
  // uid-less row) selects NOTHING — five tab stops and no panel — and a tab
  // switch silently unmounts an open editor with an unsaved draft in it.
  const [ownTab, setOwnTab] = useState<AlertDetailTab>(defaultTab);
  const tab = controlledTab ?? ownTab;
  const setTab = (next: AlertDetailTab) => {
    if (onTabChange) onTabChange(next);
    else setOwnTab(next);
  };
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
    // The Record pane fills the drawer down to its bottom edge and scrolls
    // inside (see .detail[data-fill]); every other pane flows and the drawer
    // body scrolls as before.
    <div className={styles.detail} data-fill={activeTab === "record" || undefined}>
      <div className={styles.summary}>
        {/* What it is, first: the message is the alert's sentence, the host
            (the drawer title) its subject. */}
        {row.message ? <AlertMessage text={row.message} /> : null}
        <div className={styles.badges}>
          <Badge color={severityColor(row.severity ?? "")} title={row.severity ?? "—"}>
            {row.severity ? severityDisplayLabel(row.severity) : "—"}
          </Badge>
          <Badge variant={stateBadgeVariant(state)}>
            {deadline ? `${stateLabel(state)} · ${deadline}` : stateLabel(state)}
          </Badge>
          {escalation ? <Badge variant="warning">{escalation}</Badge> : null}
        </div>
        {/* The context, as one aligned label/value list rather than a stack
            of differently-worded lines: when, who, did it page, was it
            silenced, where from. Each entry only when it has something to
            say. */}
        <dl className={styles.facts}>
          <div className={styles.fact}>
            <dt>Seen</dt>
            <dd>
              {showFirstSeen || hits > 1 ? "last " : null}
              <TimeCell epoch={row.date_epoch} compact />
              {showFirstSeen ? (
                <span className={styles.factMuted}>
                  {" · first "}
                  <time
                    dateTime={new Date(firstSeen * 1000).toISOString()}
                    title={formatAbsoluteTime(firstSeen)}
                  >
                    {trimDate(firstSeen)}
                  </time>
                </span>
              ) : null}
              {hits > 1 ? (
                <span
                  className={styles.factMuted}
                >{` · ${hits.toLocaleString("en-US")} hits`}</span>
              ) : null}
            </dd>
          </div>
          <OwnerFact record={row} itemClassName={styles.fact} />
          {lastDelivery ? (
            <div className={styles.fact} data-tone={lastDelivery.variant}>
              <dt>Notified</dt>
              <dd>
                {/* The whole value opens the Deliveries tab: the next question
                    after "when was it last sent" is "and before that?". Plain
                    text inside, not a TimeCell — its tooltip trigger would be
                    an interactive element nested in this button. */}
                <button
                  type="button"
                  className={styles.factButton}
                  title={`${formatAbsoluteTime(lastDelivery.epoch)} — show deliveries`}
                  aria-label={`${notifiedText}. Show deliveries`}
                  onClick={() => handleTabChange("deliveries")}
                >
                  {lastDelivery.variant === "error" ? "Failed " : null}
                  {relativeAgo(lastDelivery.epoch)}
                  {lastDelivery.via ? (
                    <span className={styles.factMuted}>{` via ${lastDelivery.via}`}</span>
                  ) : null}
                  {lastDelivery.batchCount ? (
                    <span className={styles.factMuted}>
                      {` · batch of ${lastDelivery.batchCount}`}
                    </span>
                  ) : null}
                </button>
              </dd>
            </div>
          ) : null}
          {silencedBy ? (
            <div className={styles.fact}>
              <dt>Silenced</dt>
              <dd>
                <span className={styles.factMuted}>by </span>
                <Link
                  to="/web/snoozes"
                  search={{ search: nameQuery(silencedBy) }}
                  className={styles.factLink}
                >
                  {silencedBy}
                </Link>
              </dd>
            </div>
          ) : null}
          {row.source || row.process || sourceUrl ? (
            <div className={styles.fact}>
              <dt>Source</dt>
              <dd>
                {[row.source, row.process].filter(Boolean).join(" · ")}
                {sourceUrl ? (
                  <>
                    {row.source || row.process ? (
                      <span className={styles.factMuted}> · </span>
                    ) : null}
                    <a
                      href={sourceUrl}
                      target="_blank"
                      rel="noreferrer"
                      className={styles.factLink}
                    >
                      Open ↗
                    </a>
                  </>
                ) : null}
              </dd>
            </div>
          ) : null}
          {labels.length > 0 ? (
            <div className={styles.fact}>
              <dt>Labels</dt>
              <dd className={styles.labelsValue}>
                <LabelList labels={labels} />
              </dd>
            </div>
          ) : null}
        </dl>
        {/* A pointer to the Analysis tab, so it steps aside while that tab is
            open: the pane leads with the same verdict and headline. */}
        {rootCause && headline !== "" && activeTab !== "analysis" ? (
          <button
            type="button"
            className={styles.analysisLine}
            title={headline}
            // Spelled out so the verdict chip (whose hint is a title, not a
            // name) and the headline read as one sentence.
            aria-label={[
              ...(verdict !== undefined ? [planStatusLabel(verdict)] : []),
              analysisNoun,
              headline,
            ].join(" · ")}
            onClick={() => handleTabChange("analysis")}
          >
            {verdict !== undefined ? <VerdictChip status={verdict} /> : null}
            {/* One run of text, so the ellipsis clips the headline and never
                the noun in front of it. */}
            <span className={styles.analysisText}>
              <span className={styles.analysisNoun}>{analysisNoun}</span>
              {" · "}
              {headline}
            </span>
            <Icon name="chevron-right" size={14} className={styles.analysisChevron!} />
          </button>
        ) : null}
      </div>

      <Tabs value={activeTab} onValueChange={handleTabChange}>
        <TabList>
          <TabTrigger value="timeline">Timeline</TabTrigger>
          <TabTrigger value="flow">Flow</TabTrigger>
          {/* Analysis sits between Flow and Deliveries: Flow says what the
              pipeline did, Analysis says why the alert fired at all. */}
          {/* A dot, not a word, says "there is one": the accessible name stays
              "Analysis" (the header line already announces it), and the
              confidence that used to ride here is said once, in the pane. */}
          {showAnalysis ? (
            <TabTrigger value="analysis">
              Analysis
              {rootCause ? (
                <span className={styles.tabMarker} data-slot="analysis-marker" aria-hidden="true" />
              ) : null}
            </TabTrigger>
          ) : null}
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
          <AnalysisTab
            uid={row.uid}
            lastEpoch={row.date_epoch}
            onEditingChange={handleEditingChange}
          />
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
          <JsonViewer value={cleaned} searchable fill />
        </TabPanel>
      </Tabs>

      <DiscardAnalysisDraftDialog
        open={pendingTab !== null}
        onOpenChange={(open) => {
          if (!open) setPendingTab(null);
        }}
        onDiscard={() => {
          if (pendingTab !== null) {
            // Said before the switch, so a host guarding navigation away
            // from the editor does not ask a second time for the draft the
            // operator just discarded.
            handleEditingChange(false);
            setTab(pendingTab);
          }
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
