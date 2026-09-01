// The dashboard's primary panel: how much of what arrived never reached
// anyone, and which rule or filter did it.
//
// Snooze's claim is "fewer, better notifications", and until now the two
// panels that evidence it — throttled-by-rule and snoozed-by-filter — were
// #10 and #11, below the fold, next to a dozen co-equal breakdowns. They are
// merged here into one panel that leads with the share of the ingest stream
// that was suppressed, then names the rules and filters responsible.
import { useMemo } from "react";
import { BarChart } from "@/shared/chart/BarChart";
import { DistributionBar, type DistributionDatum } from "@/shared/chart/DistributionBar";
import { chartToken, seriesColor } from "@/shared/chart/theme";
import { capDistributions } from "./chart-format";
import { PanelEmpty, PanelHint, PanelTitle } from "./Panel";
import { countersEmpty } from "./empty-copy";
import type { StatsCounters } from "./types";
import styles from "./NoiseRemoved.module.css";

/** Top-slice for the two breakdowns. Keeps the panel scannable in one look;
 *  the count of what was cut is spelled out next to each heading. */
const BREAKDOWN_CAP = 6;

export type NoiseRemovedProps = {
  /** Events ingested in the window (snapshot.total_hits). */
  ingested: number;
  /** Throttled event counts keyed by aggregate-rule name. */
  throttled: Record<string, number>;
  /** Snoozed event counts keyed by snooze-filter name. */
  snoozed: Record<string, number>;
  windowLabel: string;
  counters?: StatsCounters;
  theme: "light" | "dark";
};

export function NoiseRemoved({
  ingested,
  throttled,
  snoozed,
  windowLabel,
  counters,
  theme,
}: NoiseRemovedProps) {
  const throttledTotal = sum(throttled);
  const snoozedTotal = sum(snoozed);
  const suppressed = throttledTotal + snoozedTotal;
  // Suppressed events are counted alongside ingest, not carved out of it, so
  // clamp rather than let a rounding/ordering artefact print a negative.
  const delivered = Math.max(ingested - suppressed, 0);
  const share = ingested > 0 ? Math.round((suppressed / ingested) * 100) : 0;

  const split: DistributionDatum[] = useMemo(
    () => [
      { label: "Throttled", value: throttledTotal, color: seriesColor("Throttled") },
      { label: "Snoozed", value: snoozedTotal, color: seriesColor("Snoozed") },
      { label: "Delivered", value: delivered, color: chartToken("--text-muted") },
    ],
    // theme is a dep so the resolved colours refresh on toggle.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [throttledTotal, snoozedTotal, delivered, theme],
  );

  return (
    <>
      <PanelTitle icon="filter">Noise removed</PanelTitle>
      <PanelHint>{`${windowLabel}, across everything ingested`}</PanelHint>

      {ingested === 0 && suppressed === 0 ? (
        <PanelEmpty {...countersEmpty(counters, windowLabel, "events")} />
      ) : (
        <>
          <p className={styles.headline}>
            <b className={styles.value}>{suppressed.toLocaleString()}</b>
            <span className={styles.caption}>
              {suppressed === 0
                ? `of ${ingested.toLocaleString()} events suppressed — everything was delivered`
                : `of ${ingested.toLocaleString()} events suppressed · ${share}% of the stream`}
            </span>
          </p>

          <DistributionBar data={split} ariaLabel="Ingested events by outcome" />

          <div className={styles.breakdowns}>
            <Breakdown
              heading="Throttled by rule"
              data={throttled}
              seriesLabel="Throttled"
              emptyTitle="No throttling"
              emptyText="No aggregate rule rate-limited an alert in this window."
              theme={theme}
            />
            <Breakdown
              heading="Snoozed by filter"
              data={snoozed}
              seriesLabel="Snoozed"
              emptyTitle="No snoozes applied"
              emptyText="No snooze filter matched an incoming alert in this window."
              theme={theme}
            />
          </div>
        </>
      )}
    </>
  );
}

function Breakdown({
  heading,
  data,
  seriesLabel,
  emptyTitle,
  emptyText,
  theme,
}: {
  heading: string;
  data: Record<string, number>;
  seriesLabel: string;
  emptyTitle: string;
  emptyText: string;
  theme: "light" | "dark";
}) {
  const { keys, capped, total } = capDistributions([data], BREAKDOWN_CAP);
  return (
    <section className={styles.breakdown}>
      <h3 className={styles.breakdownHeading}>
        {heading}
        {total > BREAKDOWN_CAP ? (
          <span className={styles.cap}>{`top ${BREAKDOWN_CAP} of ${total}`}</span>
        ) : null}
      </h3>
      {keys.length > 0 ? (
        <BarChart
          horizontal
          sort="value"
          theme={theme}
          ariaLabel={`${heading}, event count`}
          height={Math.max(140, keys.length * 26)}
          series={[{ label: seriesLabel, color: seriesColor(seriesLabel), data: capped[0]! }]}
        />
      ) : (
        <PanelEmpty compact title={emptyTitle} description={emptyText} />
      )}
    </section>
  );
}

const sum = (m: Record<string, number>) => Object.values(m).reduce((a, b) => a + b, 0);
