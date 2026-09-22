// The dashboard's second view: the open alerts somebody has already worked out
// the cause of.
//
// The alerts table answers "what is firing"; this answers "what has been
// explained, and can any of it be handed to a machine". It is a ranked list of
// real links rather than a chart — every row's point is to be opened — and the
// one number worth reading at a glance (how much of the queue is explained)
// leads the view as a sentence rather than a tile, because the Right-now tile
// that brought the reader here already said it.
//
// Live, not windowed: the rows are the record store as it stands right now, so
// the page's time-range picker is not on screen in this view and the hint says
// "right now" instead of repeating a window.
import { useMemo, useState } from "react";
import { Link } from "@tanstack/react-router";
import { Badge } from "@/shared/ui/Badge";
import { Card } from "@/shared/ui/Card";
import { Icon } from "@/shared/icons/Icon";
import { InlineError } from "@/shared/ui/InlineError";
import { Skeleton } from "@/shared/ui/Skeleton";
import { TimeCell } from "@/shared/ui/TimeCell";
import { describeError } from "@/lib/api/errorMessage";
import { severityColor } from "@/lib/format/severity-color";
import { severityDisplayLabel } from "@/features/alerts/format";
import { ConfidenceBadge } from "@/features/alerts/analysis/ConfidenceBadge";
import {
  CONFIDENCE_LEVELS,
  confidenceLabel,
  type Confidence,
} from "@/features/alerts/analysis/enums";
import { PanelEmpty, PanelHint } from "./Panel";
import { ANALYSES_ROW_CAP, useAnalysedOpenAlerts, useOpenAlertCount } from "./analyses-query";
import {
  UNANALYSED_OPEN_SEARCH,
  matchesFilters,
  toAnalysedRow,
  uidSearch,
  type AutomatableFilter,
} from "./analysis-rows";
import styles from "./AnalysesView.module.css";

const AUTOMATABLE_CHIPS: { id: AutomatableFilter; label: string }[] = [
  { id: "any", label: "Any" },
  { id: "yes", label: "Yes" },
  { id: "no", label: "No" },
];

/** Column legend, matched by `.head` / `.rowLink`'s shared grid template. */
const COLUMNS: ReadonlyArray<{ id: string; label: string; title?: string }> = [
  { id: "sev", label: "Sev", title: "Severity" },
  { id: "host", label: "Host" },
  { id: "alertname", label: "Alert" },
  { id: "cause", label: "Cause" },
  { id: "confidence", label: "Confidence" },
  { id: "steps", label: "Steps" },
  { id: "automatable", label: "Auto", title: "Automatable" },
  { id: "analysedAt", label: "Analysed" },
  { id: "by", label: "By" },
];

export function AnalysesView() {
  // Local, deliberately not URL-synced: these narrow a list, not a page, and a
  // dashboard link that carried somebody else's chip state would be a worse
  // share than one that opens on everything.
  const [confidences, setConfidences] = useState<readonly Confidence[]>(CONFIDENCE_LEVELS);
  const [automatable, setAutomatable] = useState<AutomatableFilter>("any");

  const query = useAnalysedOpenAlerts();
  // The denominator, over the analysed list's own population minus the
  // analysis clause — NOT the ACTIVE_ALERTS probe behind the sidebar badge and
  // the default alerts tab, which drops the acknowledged and snoozed rows this
  // list keeps. Reading the ratio off two differently-measured populations is
  // how "7 analysed of 6 open" became the ordinary end state. Same hook the
  // tile calls, so the two can never disagree (React Query dedupes the key).
  const open = useOpenAlertCount(true);
  const openCount = open.data?.meta.total;

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
      <h2 className={styles.headline}>
        <span className={styles.count}>{analysed}</span>
        {" analysed"}
        {/* Withheld, not defaulted: "2 analysed of 0 open" while the count is
            in flight is a claim, and a wrong one. */}
        {openCount === undefined ? null : (
          <>
            {" of "}
            {/* The open half links to what is still unexplained — the work this
                view does not cover — over the same population the denominator
                counted, so the link and the number agree. */}
            <Link
              className={styles.openLink}
              to="/web/alerts"
              search={{ tab: "all", search: UNANALYSED_OPEN_SEARCH }}
            >
              {`${openCount} open`}
            </Link>
            {/* "Open" here is wider than the alerts page's default tab, and the
                difference is the whole reason the ratio used to read wrong. */}
            <span className={styles.qualifier}>{" \u2014 acknowledged and snoozed included"}</span>
          </>
        )}
      </h2>

      {analysed > ANALYSES_ROW_CAP ? (
        <PanelHint>{`Top ${ANALYSES_ROW_CAP} of ${analysed}`}</PanelHint>
      ) : (
        <PanelHint>Right now, newest analysis first</PanelHint>
      )}

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
            <div className={styles.table}>
              {/* A column legend for the eye only: every cell below already
                  carries its own meaning in text (the badges spell their level
                  out, the step count and the automatable mark have sr-only
                  nouns), so announcing nine extra words per list would be
                  noise. */}
              <div className={styles.head} aria-hidden="true">
                {COLUMNS.map((c) => (
                  <span
                    key={c.id}
                    className={`${styles.headCell} ${styles[c.id] ?? ""}`}
                    {...(c.title ? { title: c.title } : {})}
                  >
                    {c.label}
                  </span>
                ))}
              </div>

              <ul className={styles.list}>
                {visible.map((row) => (
                  <li key={row.uid} className={styles.row}>
                    {/* tab=all, not the default lifecycle tab: an analysed
                        alert is often already acknowledged, and the drawer
                        closes itself when the uid isn't on the page it lands
                        on. */}
                    {/* `search` pins the table to this one row: the alerts
                        page fetches the newest page by date_epoch and closes a
                        drawer whose uid is not on it — exactly the rows this
                        view ranks first (old alert, fresh analysis). */}
                    <Link
                      className={styles.rowLink}
                      to="/web/alerts"
                      search={{
                        tab: "all",
                        record: row.uid,
                        analysis: true,
                        search: uidSearch(row.uid),
                      }}
                    >
                      <span className={styles.sev}>
                        <Badge
                          className={styles.sevBadge!}
                          color={severityColor(row.severity)}
                          title={row.severity || "—"}
                        >
                          {row.severity ? severityDisplayLabel(row.severity) : "—"}
                        </Badge>
                      </span>
                      <span className={styles.host}>{row.host || row.uid}</span>
                      <span className={styles.alertname}>{row.alertname || "—"}</span>
                      <span className={styles.cause} title={row.summary}>
                        {row.summary}
                      </span>
                      <span className={styles.confidence}>
                        {/* Absent on a subtree that carries no level this app
                            knows. The row still lists — it was counted. */}
                        {row.confidence === undefined ? null : (
                          <ConfidenceBadge confidence={row.confidence} />
                        )}
                      </span>
                      <span className={styles.steps}>
                        {row.steps}
                        {/* The noun the column header carries. Hidden from the
                            eye while the header is on screen, spoken always,
                            and printed once the row stacks and the header is
                            gone. */}
                        <span className={styles.stepsNoun}>
                          {row.steps === 1 ? " step" : " steps"}
                        </span>
                      </span>
                      <span className={styles.automatable}>
                        {/* Shown only when true, so the mark's presence is the
                            signal and the word behind it is for readers who
                            never see the icon. */}
                        {row.automatable ? (
                          <>
                            <Icon name="rotate-cw" size={12} />
                            <span className={styles.srOnly}>Automatable</span>
                          </>
                        ) : null}
                      </span>
                      <span className={styles.analysedAt}>
                        {row.analysedAt === undefined ? null : (
                          <TimeCell epoch={row.analysedAt} compact />
                        )}
                      </span>
                      <span className={styles.by}>{row.by || "—"}</span>
                    </Link>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </>
      )}
    </Card>
  );
}
