import type { StatsSnapshot, StatsTotals } from "./types";
import styles from "./StatTiles.module.css";

const sum = (m: Record<string, number>) => Object.values(m).reduce((a, b) => a + b, 0);

export function StatTiles({ snapshot, totals }: { snapshot: StatsSnapshot; totals: StatsTotals }) {
  const tiles = [
    { label: "Total", value: snapshot.total_hits },
    { label: "Open", value: snapshot.open },
    { label: "Ack", value: snapshot.ack },
    { label: "Closed", value: snapshot.closed },
    { label: "Throttled", value: sum(totals.by_throttled) },
    { label: "Snoozed", value: sum(totals.by_snoozed) },
  ];
  return (
    <div className={styles.strip}>
      {tiles.map((t) => (
        <div key={t.label} className={styles.tile}>
          <b className={styles.value}>{t.value}</b>
          <span className={styles.label}>{t.label}</span>
        </div>
      ))}
    </div>
  );
}
