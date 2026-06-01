import type { CSSProperties } from "react";
import { Icon } from "@/shared/icons/Icon";
import type { IconName } from "@/shared/icons/icon-names";
import type { StatsSnapshot, StatsTotals } from "./types";
import styles from "./StatTiles.module.css";

const sum = (m: Record<string, number>) => Object.values(m).reduce((a, b) => a + b, 0);

// Each tile drives both its left accent bar and its icon from one CSS custom
// property (`--tile-accent`), set to a theme token so it tracks light/dark.
type Tile = { label: string; value: number; icon: IconName; accent: string };

export function StatTiles({ snapshot, totals }: { snapshot: StatsSnapshot; totals: StatsTotals }) {
  const tiles: Tile[] = [
    { label: "Total", value: snapshot.total_hits, icon: "layers", accent: "var(--accent)" },
    { label: "Open", value: snapshot.open, icon: "bell", accent: "var(--severity-warning)" },
    { label: "Ack", value: snapshot.ack, icon: "check", accent: "var(--state-ack)" },
    { label: "Closed", value: snapshot.closed, icon: "check-circle", accent: "var(--severity-ok)" },
    { label: "Throttled", value: sum(totals.by_throttled), icon: "filter", accent: "var(--severity-error)" },
    { label: "Snoozed", value: sum(totals.by_snoozed), icon: "bell-off", accent: "var(--state-snooze)" },
  ];
  return (
    <div className={styles.strip}>
      {tiles.map((t) => (
        <div
          key={t.label}
          className={styles.tile}
          style={{ "--tile-accent": t.accent } as CSSProperties}
        >
          <b className={styles.value}>{t.value}</b>
          <span className={styles.label}>
            <span className={styles.icon}>
              <Icon name={t.icon} size={14} />
            </span>
            {t.label}
          </span>
        </div>
      ))}
    </div>
  );
}
