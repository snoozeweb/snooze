// DashboardSkeleton — placeholder layout shown on the dashboard's first
// load (isPending only, never on background isFetching), shaped to match the
// real grid so the page doesn't jump when data arrives: the two KPI clusters,
// the primary row (noise removed + time series), then the three supporting
// panels.
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

function ClusterSkeleton({ tiles }: { tiles: number }) {
  return (
    <div className={styles.tileSkeleton}>
      <Skeleton width="30%" height={12} />
      <div style={{ display: "flex", gap: "var(--space-4)", marginTop: "var(--space-3)" }}>
        {Array.from({ length: tiles }).map((_, i) => (
          <div key={i} style={{ flex: 1 }}>
            <Skeleton width="60%" height={24} />
            <div style={{ marginTop: "var(--space-2)" }}>
              <Skeleton width="80%" height={12} />
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

export function DashboardSkeleton() {
  return (
    <div className={styles.page} data-testid="dashboard-skeleton">
      {/* KPI strip — live cluster (3 tiles) + windowed cluster (3 tiles) */}
      <div className={styles.strip}>
        <ClusterSkeleton tiles={3} />
        <ClusterSkeleton tiles={3} />
      </div>

      {/* Row 1: noise removed + time series */}
      <div className={styles.rowPrimary}>
        <CardSkeleton height={360} />
        <CardSkeleton height={300} />
      </div>

      {/* Row 2: three supporting panels */}
      <div className={styles.rowSecondary}>
        {Array.from({ length: 3 }).map((_, i) => (
          <CardSkeleton key={i} height={200} />
        ))}
      </div>
    </div>
  );
}
