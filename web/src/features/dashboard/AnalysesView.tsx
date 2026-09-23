// The dashboard's second view: the open alerts somebody has already worked out
// the cause of.
//
// The alerts table answers "what is firing"; this answers "what has been
// explained, and can any of it be handed to a machine". It is a list of real
// links rather than a chart — every row's point is to be opened.
//
// Every analysed alert is listed, not a top-N: the view is a work queue, and a
// ratio against the open backlog belongs to the Right-now tile that brought the
// reader here, not above a list it does not describe.
//
// Each row carries the two halves of an analysis side by side — the cause and
// the plan — because they are read together: a cause without its plan is a
// diagnosis nobody can act on, and a plan without its cause is a set of
// commands nobody can justify. Rows are as tall as those two need.
//
// Not a column grid. An analysis is two blocks of prose and half a dozen
// scalars, and a seven-column table gave the scalars a column each: four
// one-line cells pinned to the top of a 250px row, printing the same
// "just now / agent-bot" down the page while the cause wrapped in a third of
// the width. So a row is a header bar — the alert on the left, every scalar set
// right on the same line — over the two things worth reading, and the whole
// width below belongs to them. Three regions, three quiet boundaries: the rule
// under the header, the rule between the cause and the plan, and the 3px
// severity rail the alerts table already paints on a row (DataTable's
// `--row-accent`). No fills, no panels, no nested cards.
//
// Live, not windowed: the rows are the record store as it stands right now, so
// the page's time-range picker is not on screen in this view.
import { useMemo, useState, type CSSProperties } from "react";
import { Link } from "@tanstack/react-router";
import { Badge } from "@/shared/ui/Badge";
import { Card } from "@/shared/ui/Card";
import { InlineError } from "@/shared/ui/InlineError";
import { Skeleton } from "@/shared/ui/Skeleton";
import { TimeCell } from "@/shared/ui/TimeCell";
import { describeError } from "@/lib/api/errorMessage";
import { severityColor, severityToken } from "@/lib/format/severity-color";
import { severityDisplayLabel } from "@/features/alerts/format";
import { AutomatableBadge } from "@/features/alerts/analysis/AutomatableBadge";
import { ConfidenceBadge } from "@/features/alerts/analysis/ConfidenceBadge";
import {
  CONFIDENCE_LEVELS,
  confidenceLabel,
  type Confidence,
} from "@/features/alerts/analysis/enums";
import { PanelEmpty, PanelHint } from "./Panel";
import { useAnalysedOpenAlerts } from "./analyses-query";
import {
  matchesFilters,
  toAnalysedRow,
  uidSearch,
  type AnalysedRow,
  type AutomatableFilter,
} from "./analysis-rows";
import styles from "./AnalysesView.module.css";

const AUTOMATABLE_CHIPS: { id: AutomatableFilter; label: string }[] = [
  { id: "any", label: "Any" },
  { id: "yes", label: "Yes" },
  { id: "no", label: "No" },
];

/** What a missing value reads as. */
const EM_DASH = "—";

/**
 * How many steps a card shows before it defers to the inspector.
 *
 * A plan may carry twenty steps. Printing all of them lets one analysis push
 * every other off the screen, which costs more than the steps are worth here:
 * this surface is for deciding which alert to open, and the one it opens has
 * the whole plan.
 */
const STEPS_SHOWN = 4;

/**
 * One analysis's plan.
 *
 * Every step is printed rather than counted — a count was only ever a promise
 * that the plan existed. The action leads, the command sits under it in mono
 * so the two kinds of thing never blur, and the risk tag appears **only above
 * low**: a plan of five `LOW` tags is five words of chrome saying nothing, and
 * it is exactly the thing an operator needs to spot when it is not low.
 */
function Plan({ row }: { row: AnalysedRow }) {
  if (row.plan.length === 0) {
    return <span className={styles.planEmpty}>{EM_DASH}</span>;
  }
  const shown = row.plan.slice(0, STEPS_SHOWN);
  const hidden = row.plan.length - shown.length;
  return (
    <ol className={styles.steps}>
      {shown.map((step, i) => (
        // Steps have no id of their own and their order IS their meaning, so
        // the index is the honest key here.
        // `display: contents` — the <li> keeps the semantics, the cells below
        // join the list's own grid, so the ordinals, the actions and the risk
        // tags align down the whole plan instead of each step measuring
        // itself. That is the part of a table worth having here.
        <li key={i} className={styles.step}>
          <span className={styles.stepNum}>{i + 1}</span>
          <span className={styles.stepAction}>{step.action || EM_DASH}</span>
          {/* The risk column sizes to content, so a plan whose every step is
              routine spends no width on it at all. */}
          {step.risk === "low" || step.risk === "" ? (
            <span className={styles.riskEmpty} />
          ) : (
            <span className={styles.risk} data-risk={step.risk}>
              {step.risk}
              <span className={styles.srOnly}> risk</span>
            </span>
          )}
          {step.command ? (
            <code className={styles.stepCommand} title={step.command}>
              {step.command}
            </code>
          ) : null}
        </li>
      ))}
      {hidden > 0 ? (
        <li className={styles.more}>{`+${hidden} more step${hidden === 1 ? "" : "s"}`}</li>
      ) : null}
    </ol>
  );
}

export function AnalysesView() {
  // Local, deliberately not URL-synced: these narrow a list, not a page, and a
  // dashboard link that carried somebody else's chip state would be a worse
  // share than one that opens on everything.
  const [confidences, setConfidences] = useState<readonly Confidence[]>(CONFIDENCE_LEVELS);
  const [automatable, setAutomatable] = useState<AutomatableFilter>("any");

  const query = useAnalysedOpenAlerts();

  const rows = useMemo(
    () => (query.data?.data ?? []).map(toAnalysedRow).filter((r) => r !== undefined),
    [query.data],
  );
  const visible = useMemo(
    () => rows.filter((r) => matchesFilters(r, confidences, automatable)),
    [rows, confidences, automatable],
  );

  const analysed = query.data?.meta.total ?? 0;

  function toggleConfidence(level: Confidence) {
    setConfidences((prev) =>
      prev.includes(level)
        ? prev.filter((c) => c !== level)
        : // Keep the canonical high→medium→low order however they were picked.
          CONFIDENCE_LEVELS.filter((c) => c === level || prev.includes(c)),
    );
  }

  if (query.isError) {
    return (
      <Card padded>
        <InlineError {...describeError(query.error, "Could not load the analysed alerts.")} />
      </Card>
    );
  }

  if (query.isPending) {
    return (
      <Card padded>
        <div className={styles.loading} aria-busy="true">
          <Skeleton height={16} width="30%" />
          <Skeleton height={14} />
          <Skeleton height={14} />
          <Skeleton height={14} width="80%" />
        </div>
      </Card>
    );
  }

  return (
    <Card padded>
      {/* The only line above the list, and only on the one occasion the fetch
          ceiling bites: a truncated list that says nothing is a list claiming
          to be the whole backlog. */}
      {analysed > rows.length ? (
        <PanelHint>{`Showing ${rows.length} of ${analysed} analysed alerts`}</PanelHint>
      ) : null}

      {rows.length === 0 ? (
        <PanelEmpty
          title="No analyses yet"
          description="The alert-rca agent loop writes a root cause and remediation plan onto open alerts."
        />
      ) : (
        <>
          <div className={styles.filters}>
            {/* The groups name themselves for the eye too: at full width, six
                chips reading "High Medium Low Any Yes No" are two questions
                with no question printed. The group's own aria-label already
                says it, so the visible copy is decoration to a screen
                reader. */}
            <div className={styles.filterGroup}>
              <span className={styles.filterLabel} aria-hidden="true">
                Confidence
              </span>
              <div className={styles.chips} role="group" aria-label="Filter by confidence">
                {CONFIDENCE_LEVELS.map((level) => (
                  <button
                    key={level}
                    type="button"
                    className={styles.chip}
                    aria-pressed={confidences.includes(level)}
                    data-active={confidences.includes(level) || undefined}
                    onClick={() => toggleConfidence(level)}
                  >
                    {confidenceLabel(level)}
                  </button>
                ))}
              </div>
            </div>
            <div className={styles.filterGroup}>
              <span className={styles.filterLabel} aria-hidden="true">
                Automatable
              </span>
              <div className={styles.chips} role="group" aria-label="Filter by automatable">
                {AUTOMATABLE_CHIPS.map((c) => (
                  <button
                    key={c.id}
                    type="button"
                    className={styles.chip}
                    aria-pressed={automatable === c.id}
                    data-active={automatable === c.id || undefined}
                    onClick={() => setAutomatable(c.id)}
                  >
                    {c.label}
                  </button>
                ))}
              </div>
            </div>
          </div>

          {visible.length === 0 ? (
            <PanelEmpty compact title="Nothing matches these filters" />
          ) : (
            <ul className={styles.list}>
              {visible.map((row) => (
                <li key={row.uid} className={styles.row}>
                  {/* tab=all, not the default lifecycle tab: an analysed alert
                      is often already acknowledged, and the drawer closes
                      itself when the uid isn't on the page it lands on. */}
                  {/* `search` pins the table to this one row: the alerts page
                      fetches the newest page by date_epoch and closes a drawer
                      whose uid is not on it — exactly the rows this view ranks
                      first (old alert, fresh analysis). */}
                  <Link
                    className={styles.rowLink}
                    to="/web/alerts"
                    search={{
                      tab: "all",
                      record: row.uid,
                      analysis: true,
                      search: uidSearch(row.uid),
                    }}
                    // The severity rail, painted the same way the alerts
                    // table paints a row's accent — so the two surfaces read
                    // as one product and a 200px-tall row still carries its
                    // urgency down its whole side.
                    style={
                      severityToken(row.severity)
                        ? ({ "--row-accent": severityToken(row.severity) } as CSSProperties)
                        : undefined
                    }
                    data-accent={severityToken(row.severity) ? "true" : undefined}
                  >
                    {/* The header: which alert on the left, everything scalar
                        set right on the same line. Two lines put a pill under a
                        pill and pushed the analysis down in every row; one line
                        reads as a header bar and gives the eye a column to run
                        confidence down. */}
                    <span className={styles.header}>
                      <span className={styles.identity}>
                        <Badge
                          className={styles.sevBadge!}
                          color={severityColor(row.severity)}
                          title={row.severity || EM_DASH}
                        >
                          {row.severity ? severityDisplayLabel(row.severity) : EM_DASH}
                        </Badge>
                        <span className={styles.host}>{row.host || row.uid}</span>
                        {/* The message, not the rule name: "/var at 94%" is
                            what fired, "NodeFilesystemAlmostOutOfSpace" is only
                            what the rule is called. The rule name stands in
                            when a record carries no message. */}
                        <span className={styles.alertname}>
                          {row.message || row.alertname || EM_DASH}
                        </span>
                      </span>

                      {/* Everything scalar, on one line. Each of these used to
                        own a column and spend it on one word. */}
                      <span className={styles.meta}>
                        {row.confidence === undefined ? null : (
                          <ConfidenceBadge
                            className={styles.confidenceBadge!}
                            confidence={row.confidence}
                          />
                        )}
                        {/* Shown only when the plan says so, so its presence is
                          the signal rather than a column of blanks. */}
                        {row.automatable ? (
                          <AutomatableBadge className={styles.metaBadge!} />
                        ) : null}
                        {/* Badges lead, then the plain facts — which keeps the
                            dot separators between text items only, never
                            hanging off the edge of a chip. */}
                        <span className={styles.metaItem}>
                          {`${row.steps} step${row.steps === 1 ? "" : "s"}`}
                        </span>
                        {row.analysedAt === undefined ? null : (
                          <span className={styles.metaItem}>
                            <TimeCell epoch={row.analysedAt} compact />
                          </span>
                        )}
                        {row.by ? <span className={styles.metaItem}>{`by ${row.by}`}</span> : null}
                      </span>
                    </span>

                    {/* The two halves, and the only two things that get width.
                        The labels replace the table header the columns used to
                        need: they travel with the row instead of scrolling
                        away from it. */}
                    <span className={styles.analysis}>
                      <span className={styles.block}>
                        <span className={styles.blockLabel}>Why</span>
                        <span className={styles.cause}>{row.summary}</span>
                      </span>
                      <span className={styles.block}>
                        <span className={styles.blockLabel}>What to do</span>
                        <Plan row={row} />
                      </span>
                    </span>
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </>
      )}
    </Card>
  );
}
