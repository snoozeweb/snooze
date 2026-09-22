import type { CSSProperties } from "react";
import { Icon } from "@/shared/icons/Icon";
import type { IconName } from "@/shared/icons/icon-names";
import type { TabId } from "@/features/alerts/tabs";
import { STATE_NOUN } from "@/features/alerts/lifecycle";
import type { StatsSnapshot, StatsTotals } from "./types";
import styles from "./StatTiles.module.css";

const sum = (m: Record<string, number>) => Object.values(m).reduce((a, b) => a + b, 0);

/** Stable id per tile — used for delta lookup and drill-down routing. */
export type TileId = "attention" | "ack" | "analysed" | "ingested" | "throttled" | "snoozed";

type Tile = {
  id: TileId;
  label: string;
  value: number;
  /**
   * What to print instead of the formatted `value` — an em-dash for a number
   * whose query failed. The tile stays on the strip either way: a tile that
   * vanishes on error reads as "zero", which is a different claim.
   */
  valueText?: string;
  /**
   * Second half of the value line, for a number that only means something
   * against another one ("12 **of 37 open**"). Muted and smaller — the tile's
   * headline is still the first number. Omitted until the other number is
   * known — "12 of 0 open" is worse than "12".
   */
  hint?: string;
  icon: IconName;
  accent: string;
  /**
   * What activating the tile does; absent → the tile is not interactive. Only
   * the live tiles get one: they count the very rows they would open, so the
   * tile and its destination can never disagree. The windowed tiles count
   * events (duplicates included, and suppressed events that were never stored
   * as alerts at all) — sending them to a list of alerts would show a
   * different number than the tile.
   */
  onActivate?: () => void;
};

export type StatTilesProps = {
  snapshot: StatsSnapshot;
  totals: StatsTotals;
  /**
   * Alerts in the working queue right now — the ACTIVE_ALERTS preset behind
   * the sidebar badge and the default alerts tab. Passed in rather than
   * derived from `snapshot.open`, which also counts snoozed and shelved rows
   * and would print a bigger number than the list it links to.
   */
  needsAttention: number;
  /**
   * The "Analysed" tile's three numbers, or nothing at all.
   *
   * Absent → the tile is off the strip entirely, which is what a viewer
   * without `ro_record` gets: the count behind it is a /record query their
   * token would 403.
   *
   * `count` is how many open alerts carry an agentic analysis right now;
   * `open` is the population it is a share of — deliberately NOT the
   * "Needs attention" number beside it, which drops the acknowledged and
   * snoozed rows the analysed count keeps (it would print "7 of 6"). Either
   * may be undefined while its query is in flight; the tile then shows what it
   * has. `error` keeps the tile with an em-dash rather than letting a failed
   * query read as a zero.
   */
  analysed?: {
    count?: number | undefined;
    open?: number | undefined;
    error?: boolean | undefined;
  };
  /** Human name of the picked window, e.g. "Last 24 hours". */
  windowLabel: string;
  /** Called when a live tile is activated, with the alerts tab to open. */
  onTileClick?: (tab: TabId) => void;
  /** Called when the "Analysed" tile is activated — the page's Analyses view. */
  onAnalysedClick?: () => void;
  /**
   * Percentage change vs. the prior window, keyed by tile id. Only the
   * windowed tiles carry one; the live tiles are point-in-time and have no
   * meaningful prior-window comparison. `null`/absent → no badge.
   */
  deltas?: Partial<Record<TileId, number | null>>;
};

/**
 * The dashboard's headline row, split into two labelled clusters because the
 * numbers come from two different places:
 *
 *   - "Right now" reads the alert store live — the same rows the alerts table
 *     shows.
 *   - the window cluster sums pipeline counters over the picked range, and
 *     leads with the product's own claim: how much of what arrived never
 *     reached anyone.
 *
 * They used to sit in one undifferentiated strip of six, which is how a live
 * "Open 8" ended up next to a windowed "Total 0".
 */
export function StatTiles({
  snapshot,
  totals,
  needsAttention,
  analysed,
  windowLabel,
  onTileClick,
  onAnalysedClick,
  deltas,
}: StatTilesProps) {
  const live: Tile[] = [
    {
      id: "attention",
      label: "Needs attention",
      value: needsAttention,
      icon: "bell",
      accent: "var(--severity-warning)",
      ...(onTileClick ? { onActivate: () => onTileClick("alerts") } : {}),
    },
    {
      id: "ack",
      label: STATE_NOUN.ack,
      value: snapshot.ack,
      icon: "check",
      // Violet, matching the state chip / timeline / feed — not the OK-green
      // severity token. Acknowledged isn't "fine", it's "someone has it".
      accent: "var(--state-ack)",
      ...(onTileClick ? { onActivate: () => onTileClick("ack") } : {}),
    },
  ];

  // Third live tile: how much of the open queue somebody has already explained.
  // The share is the whole claim — an analysed count on its own says nothing —
  // so the denominator is the same population the numerator was counted over.
  if (analysed && (analysed.count !== undefined || analysed.error)) {
    live.push({
      id: "analysed",
      label: "Analysed",
      value: analysed.count ?? 0,
      ...(analysed.error ? { valueText: "\u2014" } : {}),
      ...(analysed.open !== undefined && !analysed.error
        ? { hint: `of ${analysed.open.toLocaleString()} open` }
        : {}),
      icon: "file-text",
      accent: "var(--severity-info)",
      ...(onAnalysedClick ? { onActivate: onAnalysedClick } : {}),
    });
  }

  const windowed: Tile[] = [
    {
      id: "throttled",
      label: "Throttled",
      value: sum(totals.by_throttled),
      icon: "filter",
      accent: "var(--state-ack)",
    },
    {
      id: "snoozed",
      label: "Snoozed",
      value: sum(totals.by_snoozed),
      icon: "bell-off",
      accent: "var(--severity-warning)",
    },
    {
      id: "ingested",
      label: "Ingested",
      value: snapshot.total_hits,
      icon: "layers",
      accent: "var(--severity-info)",
    },
  ];

  return (
    <div className={styles.strip}>
      <TileGroup label="Right now" tiles={live} deltas={deltas} />
      <TileGroup label={windowLabel} tiles={windowed} deltas={deltas} />
    </div>
  );
}

function TileGroup({
  label,
  tiles,
  deltas,
}: {
  label: string;
  tiles: Tile[];
  deltas?: Partial<Record<TileId, number | null>> | undefined;
}) {
  return (
    <section className={styles.group} aria-label={label}>
      <h2 className={styles.groupLabel}>{label}</h2>
      <div className={styles.tiles}>
        {tiles.map((t) => {
          const activate = t.onActivate;
          const delta = deltas?.[t.id];
          const body = (
            <>
              <b className={styles.value}>
                {t.valueText ?? t.value.toLocaleString()}
                {/* A real space, not the hint's CSS margin: without it the
                    tile announces "7of 6 openAnalysed". */}
                {t.hint ? <span className={styles.hint}> {t.hint}</span> : null}
              </b>
              <span className={styles.label}>
                <span className={styles.icon}>
                  <Icon name={t.icon} size={14} />
                </span>
                {t.label}
                {delta != null ? <Delta pct={delta} /> : null}
              </span>
            </>
          );
          const style = { "--tile-accent": t.accent } as CSSProperties;
          return activate ? (
            <button
              key={t.id}
              type="button"
              data-tile={t.id}
              className={`${styles.tile} ${styles.clickable}`}
              style={style}
              onClick={activate}
            >
              {body}
            </button>
          ) : (
            <div key={t.id} data-tile={t.id} className={styles.tile} style={style}>
              {body}
            </div>
          );
        })}
      </div>
    </section>
  );
}

// ▲/▼ percent vs the prior window. Up = more events (neutral-critical), down
// = fewer (ok). Zero is omitted upstream; we render "0%" defensively as muted.
function Delta({ pct }: { pct: number }) {
  const rounded = Math.round(pct);
  const dir = rounded > 0 ? "up" : rounded < 0 ? "down" : "flat";
  const arrow = dir === "up" ? "▲" : dir === "down" ? "▼" : "•";
  const sign = rounded > 0 ? "+" : "";
  return (
    <span className={styles.delta} data-dir={dir} aria-label={`${sign}${rounded}% vs prior period`}>
      {arrow} {sign}
      {rounded}%
    </span>
  );
}
