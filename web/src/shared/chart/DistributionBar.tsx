// DistributionBar — a pure-CSS stacked horizontal bar with a legend.
//
// Replaces the doughnut charts on the dashboard: a doughnut buries its
// values in a tooltip and needs a canvas; a stacked bar + legend rows show
// the label, the mono count and the percentage inline, re-themes via
// tokens for free, and is keyboard-navigable when `onSegmentClick` is set.
//
// Tokens only — no hex literals. Segment/dot colours come from the caller
// (resolved CSS colour strings, typically from the chart `theme.ts`
// helpers), so the component itself stays colour-agnostic.

import { useMemo } from "react";
import styles from "./DistributionBar.module.css";

export type DistributionDatum = {
  /** Display + drill-down key. */
  label: string;
  value: number;
  /** Resolved CSS colour for this segment + its legend dot. */
  color: string;
};

export type DistributionBarProps = {
  data: DistributionDatum[];
  /** Drill-down callback; when set, segments and legend rows become buttons. */
  onSegmentClick?: (label: string) => void;
  /** Accessible name for the bar (e.g. "By severity"). */
  ariaLabel?: string;
};

const pct = (value: number, total: number) => (total > 0 ? (value / total) * 100 : 0);
const fmt = (n: number) => n.toLocaleString();

export function DistributionBar({ data, onSegmentClick, ariaLabel }: DistributionBarProps) {
  const total = useMemo(() => data.reduce((a, d) => a + d.value, 0), [data]);
  // Drop zero-value entries — they'd add invisible 0%-wide segments and
  // noisy "0" legend rows.
  const items = useMemo(() => data.filter((d) => d.value > 0), [data]);

  if (items.length === 0) return null;

  return (
    <div className={styles.root}>
      <div
        className={styles.bar}
        role="img"
        aria-label={ariaLabel ? `${ariaLabel}: ${total} total` : undefined}
      >
        {items.map((d) => {
          const width = `${pct(d.value, total)}%`;
          const title = `${d.label}: ${fmt(d.value)} (${pct(d.value, total).toFixed(1)}%)`;
          return onSegmentClick ? (
            <button
              key={d.label}
              type="button"
              className={styles.segment}
              style={{ width, background: d.color }}
              title={title}
              aria-label={title}
              onClick={() => onSegmentClick(d.label)}
            />
          ) : (
            <span
              key={d.label}
              className={styles.segment}
              style={{ width, background: d.color }}
              title={title}
            />
          );
        })}
      </div>

      <ul className={styles.legend}>
        {items.map((d) => {
          const percent = `${pct(d.value, total).toFixed(1)}%`;
          const rowContent = (
            <>
              <span className={styles.dot} style={{ background: d.color }} aria-hidden="true" />
              <span className={styles.label}>{d.label}</span>
              <span className={styles.count}>{fmt(d.value)}</span>
              <span className={styles.percent}>{percent}</span>
            </>
          );
          return (
            <li key={d.label} className={styles.row}>
              {onSegmentClick ? (
                <button
                  type="button"
                  className={styles.rowButton}
                  onClick={() => onSegmentClick(d.label)}
                >
                  {rowContent}
                </button>
              ) : (
                rowContent
              )}
            </li>
          );
        })}
      </ul>
    </div>
  );
}
