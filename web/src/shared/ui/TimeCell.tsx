import { Tooltip } from "./Tooltip";
import { formatRelativeTime, trimDate } from "@/lib/format/time";
import styles from "./TimeCell.module.css";

export type TimeCellProps = {
  /** Unix epoch in seconds (the shape Snooze records carry as `date_epoch`). */
  epoch: number | undefined;
  /** Tooltip placement; forwarded to the shared Tooltip. */
  side?: "top" | "right" | "bottom" | "left";
};

const HOUR_SECONDS = 3600;

// Hoisted so the tooltip string doesn't construct a fresh Intl.DateTimeFormat
// per cell per render. One shared formatter, locale-default like
// toLocaleString() but with explicit medium date+time styles.
const tooltipFormat = new Intl.DateTimeFormat(undefined, {
  dateStyle: "medium",
  timeStyle: "medium",
});

/**
 * TimeCell renders a Snooze epoch (seconds) the way the alerts table does —
 * `trimDate` smart text ("Today 14:32", "Nov 5th 14:32", "Jan 1st 2024") — in
 * mono tabular numerals so columns of timestamps line up. It wraps the text in
 * a semantic `<time dateTime>` element and a tooltip carrying the full locale
 * timestamp, and prefixes a relative "Nm ago" hint while the event is under an
 * hour old (the window where "how long ago" is what the operator cares about).
 *
 * Phase 2 only ships the primitive; later phases wire it into columns.tsx,
 * the audit timeline, and the snoozes/users tables.
 */
export function TimeCell({ epoch, side = "top" }: TimeCellProps) {
  if (epoch === undefined || epoch === 0) {
    return <span className={styles.cell}>—</span>;
  }

  const date = new Date(epoch * 1000);
  if (Number.isNaN(date.getTime())) {
    return <span className={styles.cell}>—</span>;
  }

  const iso = date.toISOString();
  const full = tooltipFormat.format(date);
  const now = Math.floor(Date.now() / 1000);
  const ageSeconds = now - epoch;
  // "Nm ago" prefix only for fresh, past events (< 1h old). Future epochs and
  // older ones fall through to the plain absolute trimDate text.
  const recent = ageSeconds >= 0 && ageSeconds < HOUR_SECONDS;
  // formatRelativeTime already returns the grammatical "just now" for
  // sub-second events; only the "5m" / "12s" forms take the " ago" suffix.
  const relative = recent ? formatRelativeTime(epoch) : "";
  const relativeLabel = relative === "just now" ? relative : relative ? `${relative} ago` : "";

  // The relative hint always occupies a fixed-width, right-aligned slot (see
  // .relative in the CSS) — even when empty — so the absolute timestamps after
  // it line up in a single vertical column down the table, regardless of how
  // wide the hint is or whether the row is recent enough to carry one.
  return (
    <Tooltip content={full} side={side}>
      <time className={styles.cell} dateTime={iso}>
        <span className={styles.relative}>{relativeLabel}</span>
        {trimDate(epoch)}
      </time>
    </Tooltip>
  );
}
