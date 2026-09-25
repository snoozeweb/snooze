// One analysed alert in the Analyses view.
//
// An <article>, not a link. The row used to be one big <a>: nothing inside it
// could be a control (no expand, no copy), its accessible name was the whole
// analysis read aloud, and a plan's <ol> sat inside a <span> inside the <a>.
// Now the alert's name is the heading and the heading is the link; everything
// else in the row is prose to read or an explicit control. On a phone that
// also means the row is no longer one 30-line tap target.
//
// Two states. Collapsed is the triage read, in the order the verdict layer
// (features/alerts/analysis/verdict.ts) fixes for every analysis surface: is
// there anything to do, what happened in one line, how far to trust it, and
// the shape of the plan. Expanded is the whole analysis — body, caveats, every
// step with its command to copy — so the conclusion is never cut off behind a
// line clamp with no way to reach it.
//
// Three regions, three quiet boundaries: the header (which alert, and where it
// stands), the analysis (cause beside plan on a wide row, above it on a narrow
// one), and the actions. The 3px severity rail down the side is DataTable's
// `--row-accent`, so the two surfaces read as one product. No fills, no nested
// cards.
import { memo, useCallback, useId, type CSSProperties } from "react";
import { Link } from "@tanstack/react-router";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { CodeBlock } from "@/shared/ui/Code";
import { IconButton } from "@/shared/ui/IconButton";
import { TimeCell } from "@/shared/ui/TimeCell";
import { toast } from "@/shared/ui/toast/useToast";
import { Icon } from "@/shared/icons/Icon";
import { severityColor, severityToken } from "@/lib/format/severity-color";
import { severityDisplayLabel, stateBadgeVariant, stateLabel } from "@/features/alerts/format";
import { SNOOZED_NOUN } from "@/features/alerts/lifecycle";
import type { AlertState } from "@/features/alerts/types";
import { AutomatableBadge } from "@/features/alerts/analysis/AutomatableBadge";
import { ConfidenceMeter } from "@/features/alerts/analysis/ConfidenceMeter";
import { RiskBadge } from "@/features/alerts/analysis/RiskBadge";
import { VerdictChip } from "@/features/alerts/analysis/VerdictChip";
import { groupSteps, stepWhenLabel, type IndexedStep } from "@/features/alerts/analysis/verdict";
import {
  isClosed,
  openAlertSearch,
  riskyStepCount,
  type AnalysedRow,
  type PlanStep,
  type RiskLevel,
} from "./analysis-rows";
import styles from "./AnalysisRow.module.css";

/** What a missing value reads as. */
const EM_DASH = "—";

const plural = (n: number, noun: string) => `${n} ${noun}${n === 1 ? "" : "s"}`;

/** " · 2 medium/high risk", or nothing when every step is routine. */
function riskSuffix(steps: readonly PlanStep[]): string {
  const risky = riskyStepCount(steps);
  return risky > 0 ? ` · ${risky} medium/high risk` : "";
}

/**
 * The shared risk rule (the inspector's too): low is the default answer and
 * gets no mark — a plan of five "Low risk" pills is five pills saying nothing,
 * and it buries the one step that is not low. Medium and high are spelled out.
 */
function markedRisk(risk: RiskLevel | ""): Exclude<RiskLevel, "low"> | undefined {
  return risk === "medium" || risk === "high" ? risk : undefined;
}

function CommandLine({ command }: { command: string }) {
  const copy = useCallback(() => {
    void (async () => {
      try {
        await navigator.clipboard.writeText(command);
        toast.success("Command copied");
      } catch {
        toast.error("Copy failed — select and copy manually");
      }
    })();
  }, [command]);
  // Same construction as the inspector's StepCard: the copy button hangs off
  // the wrapper, not the <pre>, because the pre is the horizontal scroll
  // container and a button inside it would slide away with a long command.
  return (
    <div className={styles.commandWrap}>
      <CodeBlock className={styles.command!}>{command}</CodeBlock>
      <IconButton
        className={styles.copy}
        icon="copy"
        label="Copy command"
        size="sm"
        onClick={copy}
      />
    </div>
  );
}

/**
 * One step, expanded. Not the inspector's StepCard: that one marks every risk,
 * low included, and this surface follows the shared rule above.
 */
function StepRow({ index, step }: { index: number; step: PlanStep }) {
  const risk = markedRisk(step.risk);
  const command = step.command.trim();
  return (
    <li className={styles.step}>
      <div className={styles.stepHead}>
        <span className={styles.stepNum}>{index}</span>
        <p className={styles.stepAction}>{step.action || EM_DASH}</p>
        {risk ? <RiskBadge risk={risk} className={styles.stepRisk!} /> : null}
      </div>
      {command === "" ? null : <CommandLine command={command} />}
    </li>
  );
}

function StepList({ steps }: { steps: readonly IndexedStep<PlanStep>[] }) {
  return (
    <ol className={styles.steps}>
      {/* Steps have no id of their own and their order IS their meaning, so the
          original index is the honest key — and the number printed, because it
          is the one an operator quotes ("step 3 failed"). */}
      {steps.map(({ step, index }) => (
        <StepRow key={index} index={index + 1} step={step} />
      ))}
    </ol>
  );
}

/**
 * The plan, collapsed: its shape in two lines. A grouped plan (steps that say
 * `when`) leads with what on-call runs now and counts the follow-ups; an
 * ungrouped one (every analysis written before the field existed) states its
 * size and shows the first step.
 */
function PlanSummary({ row }: { row: AnalysedRow }) {
  if (row.plan.length === 0) return <p className={styles.planEmpty}>No remediation plan</p>;
  const { grouped, now, followUp } = groupSteps(row.plan);
  if (grouped) {
    const first = now[0];
    const nowRisk = now.map((s) => markedRisk(s.step.risk)).find(Boolean);
    return (
      <>
        <p className={styles.planLine}>
          <span className={styles.planLabel}>{stepWhenLabel("now")}</span>
          {first ? (
            <span className={styles.planAction}>{first.step.action || EM_DASH}</span>
          ) : (
            <span className={styles.planNone}>nothing to run</span>
          )}
          {now.length > 1 ? <span className={styles.planMore}>{`+${now.length - 1}`}</span> : null}
          {nowRisk ? <RiskBadge risk={nowRisk} className={styles.stepRisk!} /> : null}
        </p>
        {followUp.length > 0 ? (
          <p className={styles.planCount}>
            {`Follow-ups: ${followUp.length}`}
            {riskSuffix(followUp.map((s) => s.step))}
          </p>
        ) : null}
      </>
    );
  }
  return (
    <>
      <p className={styles.planCount}>
        {`Plan: ${plural(row.plan.length, "step")}`}
        {riskSuffix(row.plan)}
      </p>
      <p className={styles.planLine}>
        <span className={styles.planAction}>{row.plan[0]?.action || EM_DASH}</span>
      </p>
    </>
  );
}

/** The plan, expanded: every step, under Now / Follow-up when the plan groups them. */
function PlanFull({ row, headingId }: { row: AnalysedRow; headingId: string }) {
  if (row.plan.length === 0) return <p className={styles.planEmpty}>No remediation plan</p>;
  const { grouped, now, followUp } = groupSteps(row.plan);
  if (!grouped) {
    return (
      <>
        <p className={styles.planCount}>
          {`Plan: ${plural(row.plan.length, "step")}`}
          {riskSuffix(row.plan)}
        </p>
        <StepList steps={now} />
      </>
    );
  }
  return (
    <>
      <section className={styles.group} aria-labelledby={`${headingId}-now`}>
        <h4 id={`${headingId}-now`} className={styles.subheading}>
          {stepWhenLabel("now")}
        </h4>
        {now.length > 0 ? (
          <StepList steps={now} />
        ) : (
          <p className={styles.planEmpty}>Nothing to run now</p>
        )}
      </section>
      {followUp.length > 0 ? (
        <section className={styles.group} aria-labelledby={`${headingId}-follow`}>
          <h4 id={`${headingId}-follow`} className={styles.subheading}>
            {stepWhenLabel("follow_up")}
          </h4>
          <StepList steps={followUp} />
        </section>
      ) : null}
    </>
  );
}

export type AnalysisRowProps = {
  row: AnalysedRow;
  expanded: boolean;
  /** Whether this row's article is the list's one tab stop (roving tabindex). */
  tabStop: boolean;
  /** The server's count of the analysed population — decides the link's search. */
  analysedTotal: number;
  onToggle: (uid: string) => void;
  /** Focus entered this row: it becomes the tab stop. */
  onActivate: (uid: string) => void;
  registerArticle: (uid: string, el: HTMLElement | null) => void;
};

export const AnalysisRow = memo(function AnalysisRow({
  row,
  expanded,
  tabStop,
  analysedTotal,
  onToggle,
  onActivate,
  registerArticle,
}: AnalysisRowProps) {
  const id = useId();
  const titleId = `${id}-title`;
  const bodyId = `${id}-body`;
  const uid = row.uid;
  const accent = severityToken(row.severity);
  const state = row.state as AlertState;
  const { author } = row;
  // Something to expand into. A degraded row (no body, no caveats, no plan)
  // has none, so it gets no button — and its headline is never clamped, since
  // there would be no way to read the rest.
  const hasMore = row.body !== "" || row.caveats.length > 0 || row.plan.length > 0;

  // tab=all, not the default lifecycle tab: an analysed alert is often already
  // acknowledged, and the drawer closes itself when the uid isn't on the page
  // it lands on. `search` selects the analysed set (or pins this uid once the
  // set outgrows one page) — see `openAlertSearch`.
  const linkSearch = {
    tab: "all",
    record: uid,
    pane: "analysis",
    search: openAlertSearch(uid, analysedTotal),
  } as const;

  const articleRef = useCallback(
    (el: HTMLElement | null) => registerArticle(uid, el),
    [registerArticle, uid],
  );

  return (
    <li
      className={styles.row}
      // The severity rail, painted the way the alerts table paints a row's
      // accent — so a tall row still carries its urgency down its whole side.
      style={accent ? ({ "--row-accent": accent } as CSSProperties) : undefined}
      data-accent={accent ? "true" : undefined}
      // A closed alert is finished work: the row keeps its severity badge but
      // its rail goes quiet (AnalysisRow.module.css), so the list's urgency
      // down its left edge is only the alerts still in play.
      data-closed={isClosed(row) ? "true" : undefined}
    >
      {/* Focusable so J/K can land on the row itself (the list's roving tab
          stop); the heading names it. See AnalysesView for the keys. */}
      <article
        ref={articleRef}
        className={styles.article}
        aria-labelledby={titleId}
        aria-keyshortcuts="J K Enter E"
        // The feed pattern: a row article is the keyboard stop J/K move
        // between; its controls stay separately tabbable.
        // eslint-disable-next-line jsx-a11y/no-noninteractive-tabindex
        tabIndex={tabStop ? 0 : -1}
        data-uid={uid}
        onFocus={() => onActivate(uid)}
      >
        <header className={styles.header}>
          <Badge
            className={styles.sevBadge!}
            color={severityColor(row.severity)}
            title={row.severity || EM_DASH}
          >
            {row.severity ? severityDisplayLabel(row.severity) : EM_DASH}
          </Badge>
          <h3 id={titleId} className={styles.title}>
            <Link className={styles.titleLink} to="/web/alerts" search={linkSearch}>
              <span className={styles.host} data-host="">
                {row.host || uid}
              </span>
              {/* Heard, not seen: without it the heading reads
                  "srv-victoria1/var at 94%" as one word. */}
              <span className={styles.srOnly}>, </span>
              {/* The message, not the rule name: "/var at 94%" is what fired,
                  "NodeFilesystemAlmostOutOfSpace" is only what the rule is
                  called. The rule name stands in when there is no message. */}
              <span className={styles.message}>{row.message || row.alertname || EM_DASH}</span>
            </Link>
          </h3>
          {/* Where the alert stands. The population keeps acknowledged and
              snoozed rows on purpose, so the row has to say which it is. */}
          <div className={styles.alertState}>
            <Badge variant={stateBadgeVariant(state)}>{stateLabel(state)}</Badge>
            {row.snoozed ? <Badge variant="muted">{SNOOZED_NOUN}</Badge> : null}
            {row.firedAt !== undefined ? (
              <span className={styles.fired}>
                fired <TimeCell epoch={row.firedAt} compact />
              </span>
            ) : null}
          </div>
        </header>

        <div id={bodyId} className={styles.body}>
          <div className={styles.cause}>
            {/* The verdict leads the headline on its line rather than sitting
                above it: it is the first word of the answer, not a label over
                it. */}
            <div className={styles.lead}>
              {row.status ? <VerdictChip status={row.status} className={styles.verdict!} /> : null}
              <p className={styles.headline} data-clamp={!expanded && hasMore ? "true" : undefined}>
                {row.headline}
              </p>
            </div>
            {/* Two elements for one line: the outer clips, the inner is
                shifted left by one separator — see the CSS for why. */}
            <div className={styles.meta}>
              <p className={styles.metaLine}>
                {author.kind === "human" ? (
                  <span
                    className={styles.metaItem}
                  >{`Written by ${author.by || "an operator"}`}</span>
                ) : (
                  <>
                    {/* Labelled as a machine's conclusion, always: unlabelled, it
                      is read with a human's authority. */}
                    <span
                      className={styles.metaItem}
                      title={row.by ? `Stored by ${row.by}` : undefined}
                    >
                      AI analysis
                    </span>
                    {author.tool ? <span className={styles.metaItem}>{author.tool}</span> : null}
                  </>
                )}
                {row.analysedAt !== undefined ? (
                  <span className={styles.metaItem}>
                    {/* A no-break space: a trailing plain one is dropped by the
                      flex item it sits in. */}
                    {"analysed\u00a0"}
                    <TimeCell epoch={row.analysedAt} compact />
                  </span>
                ) : null}
                {row.confidence ? (
                  <span className={styles.metaItem}>
                    <ConfidenceMeter confidence={row.confidence} />
                  </span>
                ) : null}
                {row.caveats.length > 0 ? (
                  <span className={styles.metaItem}>{plural(row.caveats.length, "caveat")}</span>
                ) : null}
                {row.stale ? (
                  <span
                    className={`${styles.metaItem} ${styles.stale}`}
                    title="The alert fired again after this analysis was written — it may describe an earlier incident"
                  >
                    <Icon name="refresh" size={12} />
                    Fired again since
                  </span>
                ) : null}
              </p>
            </div>
            {expanded && row.body !== "" ? (
              <div className={styles.prose}>
                {row.body.split(/\n\s*\n/).map((para, i) => (
                  // Paragraphs of one text have no identity but their order.
                  <p key={i}>{para}</p>
                ))}
              </div>
            ) : null}
            {expanded && row.caveats.length > 0 ? (
              <section className={styles.group} aria-labelledby={`${id}-caveats`}>
                <h4 id={`${id}-caveats`} className={styles.subheading}>
                  Caveats
                </h4>
                <ul className={styles.caveats}>
                  {row.caveats.map((c, i) => (
                    <li key={i}>{c}</li>
                  ))}
                </ul>
              </section>
            ) : null}
          </div>

          <div className={styles.plan}>
            {expanded ? <PlanFull row={row} headingId={id} /> : <PlanSummary row={row} />}
            {/* Shown only when the plan claims it, so its presence is the
                signal rather than a column of "Manual". */}
            {row.automatable ? <AutomatableBadge className={styles.automatable!} /> : null}
          </div>
        </div>

        <div className={styles.actions}>
          {hasMore ? (
            <Button
              variant="ghost"
              size="sm"
              aria-expanded={expanded}
              aria-controls={bodyId}
              trailingIcon={expanded ? "chevron-up" : "chevron-down"}
              onClick={() => onToggle(uid)}
            >
              {expanded ? "Collapse" : "Expand"}
            </Button>
          ) : null}
          <Link
            className={styles.open}
            to="/web/alerts"
            search={linkSearch}
            aria-describedby={titleId}
          >
            Open alert
            <Icon name="chevron-right" size={14} />
          </Link>
        </div>
      </article>
    </li>
  );
});
