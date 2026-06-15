import type { CSSProperties } from "react";
import { Icon } from "@/shared/icons/Icon";
import type { IconName } from "@/shared/icons/icon-names";
import type { TabId } from "@/features/alerts/tabs";
import type { StatsSnapshot, StatsTotals } from "./types";
import styles from "./StatTiles.module.css";

const sum = (m: Record<string, number>) => Object.values(m).reduce((a, b) => a + b, 0);

/** Stable id per tile — used for delta lookup and drill-down routing. */
export type TileId = "total" | "open" | "ack" | "closed" | "throttled" | "snoozed";

// Each tile drives both its left accent bar and its icon from one CSS custom
// property (`--tile-accent`), set to a theme token so it tracks light/dark.
type Tile = {
  id: TileId;
  label: string;
  value: number;
  icon: IconName;
  accent: string;
  /**
   * Alerts tab to drill into, or null when the tile isn't queryable.
   * `undefined` → the default landing tab (Open). The backend exposes no
   * record-level "throttled" field, so the Throttled tile has no target.
   */
  tab?: TabId | null;
};

export type StatTilesProps = {
  snapshot: StatsSnapshot;
  totals: StatsTotals;
  /**
   * Called when a queryable tile is activated, with the alerts tab to open.
   * When omitted, tiles render as plain (non-interactive) cards.
   */
  onTileClick?: (tab: TabId) => void;
  /**
   * Percentage change vs. the prior window, keyed by tile id. Only the
   * range-scoped summed-series tiles (throttled, snoozed) carry a delta;
   * live-snapshot tiles (total/open/ack/closed) are point-in-time and have
   * no meaningful prior-window comparison. `null`/absent → no badge.
   */
  deltas?: Partial<Record<TileId, number | null>>;
};

export function StatTiles({ snapshot, totals, onTileClick, deltas }: StatTilesProps) {
  const tiles: Tile[] = [
    {
      id: "total",
      label: "Total",
      value: snapshot.total_hits,
      icon: "layers",
      accent: "var(--accent)",
      tab: "all",
    },
    {
      id: "open",
      label: "Open",
      value: snapshot.open,
      icon: "bell",
      accent: "var(--severity-warning)",
      // tab omitted → drills to the default landing tab ("alerts").
    },
    {
      id: "ack",
      label: "Ack",
      value: snapshot.ack,
      icon: "check",
      accent: "var(--severity-ok)",
      tab: "ack",
    },
    {
      id: "closed",
      label: "Closed",
      value: snapshot.closed,
      icon: "check-circle",
      accent: "var(--state-closed)",
      tab: "closed",
    },
    {
      id: "throttled",
      label: "Throttled",
      value: sum(totals.by_throttled),
      icon: "filter",
      accent: "var(--severity-error)",
      tab: null, // no queryable record field — non-clickable
    },
    {
      id: "snoozed",
      label: "Snoozed",
      value: sum(totals.by_snoozed),
      icon: "bell-off",
      accent: "var(--state-snooze)",
      tab: "snoozed",
    },
  ];

  return (
    <div className={styles.strip}>
      {tiles.map((t) => {
        const clickable = onTileClick != null && t.tab !== null;
        const delta = deltas?.[t.id];
        const body = (
          <>
            <b className={styles.value}>{t.value}</b>
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
            className={`${styles.tile} ${styles.clickable}`}
            style={style}
            onClick={() => onTileClick(t.tab ?? "alerts")}
          >
            {body}
          </button>
        ) : (
          <div key={t.id} className={styles.tile} style={style}>
            {body}
          </div>
        );
      })}
    </div>
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
