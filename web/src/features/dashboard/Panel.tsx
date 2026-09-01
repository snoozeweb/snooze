// Panel primitives shared by every dashboard card: the title row, the
// one-line provenance hint under it, and the empty state.
//
// The hint is not decoration. Half the numbers on this page are live (the
// record store as it stands right now) and half are counter-backed sums over
// the picked window; printed side by side without saying which is which they
// read as contradictions — the "TOTAL 0 next to OPEN 8" bug. Every panel
// states its source in one line.
import { Icon } from "@/shared/icons/Icon";
import type { IconName } from "@/shared/icons/icon-names";
import styles from "./Panel.module.css";

export function PanelTitle({ icon, children }: { icon: IconName; children: string }) {
  return (
    <h2 className={styles.title}>
      <Icon name={icon} size={14} />
      {children}
    </h2>
  );
}

export function PanelHint({ children }: { children: string }) {
  return <p className={styles.hint}>{children}</p>;
}

export type PanelEmptyProps = {
  title: string;
  description?: string;
  /** Shorter vertical footprint for empties inside a panel section. */
  compact?: boolean;
};

export function PanelEmpty({ title, description, compact }: PanelEmptyProps) {
  return (
    <div
      className={compact ? `${styles.empty} ${styles.emptyCompact}` : styles.empty}
      role="status"
    >
      <p className={styles.emptyTitle}>{title}</p>
      {description ? <p className={styles.emptyText}>{description}</p> : null}
    </div>
  );
}
