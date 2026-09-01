import type { CSSProperties } from "react";
import { Icon } from "@/shared/icons/Icon";
import type { IconName } from "@/shared/icons/icon-names";
import type { TabId } from "@/features/alerts/tabs";
import type { StatsSnapshot, StatsTotals } from "./types";
import styles from "./StatTiles.module.css";

const sum = (m: Record<string, number>) => Object.values(m).reduce((a, b) => a + b, 0);

/** Stable id per tile — used for delta lookup and drill-down routing. */
export type TileId = "attention" | "ack" | "ingested" | "throttled" | "snoozed";

type Tile = {
  id: TileId;
  label: string;
  value: number;
  icon: IconName;
  accent: string;
  /**
   * Alerts tab to drill into. Only the live tiles have one: they count the
   * very rows the alerts table lists, so the tile and the list can never
   * disagree. The windowed tiles count events (duplicates included, and
   * suppressed events that were never stored as alerts at all) — sending them
   * to a list of alerts would show a different number than the tile.
   */
  tab?: TabId;
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
  /** Human name of the picked window, e.g. "Last 24 hours". */
  windowLabel: string;
  /** Called when a live tile is activated, with the alerts tab to open. */
  onTileClick?: (tab: TabId) => void;
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
  windowLabel,
  onTileClick,
  deltas,
}: StatTilesProps) {
  const live: Tile[] = [
    {
      id: "attention",
      label: "Needs attention",
      value: needsAttention,
      icon: "bell",
      accent: "var(--severity-warning)",
      tab: "alerts",
    },
    {
      id: "ack",
      label: "Acknowledged",
      value: snapshot.ack,
      icon: "check",
      accent: "var(--severity-ok)",
      tab: "ack",
    },
  ];

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
      <TileGroup label="Right now" tiles={live} onTileClick={onTileClick} deltas={deltas} />
      <TileGroup label={windowLabel} tiles={windowed} deltas={deltas} />
    </div>
  );
}

function TileGroup({
  label,
  tiles,
  onTileClick,
  deltas,
}: {
  label: string;
  tiles: Tile[];
  onTileClick?: ((tab: TabId) => void) | undefined;
  deltas?: Partial<Record<TileId, number | null>> | undefined;
}) {
  return (
    <section className={styles.group} aria-label={label}>
      <h2 className={styles.groupLabel}>{label}</h2>
      <div className={styles.tiles}>
        {tiles.map((t) => {
          const tab = t.tab;
          const clickable = onTileClick != null && tab != null;
          const delta = deltas?.[t.id];
          const body = (
            <>
              <b className={styles.value}>{t.value.toLocaleString()}</b>
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
          return clickable ? (
            <button
              key={t.id}
              type="button"
              data-tile={t.id}
              className={`${styles.tile} ${styles.clickable}`}
              style={style}
              onClick={() => onTileClick(tab)}
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
