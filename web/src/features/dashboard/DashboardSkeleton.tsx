// DashboardSkeleton — placeholder layout shown on the dashboard's first
// load (isPending only, never on background isFetching), shaped to match
// the real grid so the page doesn't jump when data arrives: a 6-tile KPI
// strip, the row-1 hero + activity split, then two 4-up card rows.
import { Card } from "@/shared/ui/Card";
import { Skeleton } from "@/shared/ui/Skeleton";
import styles from "./DashboardPage.module.css";

function CardSkeleton({ height }: { height: number }) {
  return (
    <Card padded>
      <Skeleton width="40%" height={12} />
      <div style={{ marginTop: "var(--space-3)" }}>
        <Skeleton width="100%" height={height} />
      </div>
    </Card>
  );
}

export function DashboardSkeleton() {
  return (
    <div className={styles.page} data-testid="dashboard-skeleton">
      {/* KPI strip — 6 tiles */}
      <div className={styles.strip}>
        {Array.from({ length: 6 }).map((_, i) => (
          <div key={i} className={styles.tileSkeleton}>
            <Skeleton width="50%" height={22} />
            <div style={{ marginTop: "var(--space-2)" }}>
              <Skeleton width="70%" height={12} />
            </div>
          </div>
        ))}
      </div>

      {/* Row 1: hero chart + activity feed */}
      <div className={styles.row1}>
        <CardSkeleton height={280} />
        <CardSkeleton height={280} />
      </div>

      {/* Row 2: 4 panels */}
      <div className={styles.row2}>
        {Array.from({ length: 4 }).map((_, i) => (
          <CardSkeleton key={i} height={200} />
        ))}
      </div>

      {/* Row 3: 4 panels */}
      <div className={styles.row3}>
        {Array.from({ length: 4 }).map((_, i) => (
          <CardSkeleton key={i} height={200} />
        ))}
      </div>
    </div>
  );
}
