import { useEffect, useRef } from "react";
import { Badge } from "@/shared/ui/Badge";
import { Card } from "@/shared/ui/Card";
import { Code } from "@/shared/ui/Code";
import { CollapsibleSection } from "@/shared/ui/CollapsibleSection";
import { Spinner } from "@/shared/ui/Spinner";
import { useAnnounce } from "@/shared/a11y/LiveAnnouncer";
import { useClusterStatus } from "./api";
import type { ClusterMember } from "./types";
import { clusterVerdict } from "./verdict";
import styles from "./StatusPage.module.css";

function memberBadgeVariant(status: ClusterMember["status"]): "ok" | "warning" | "critical" {
  if (status === "ok") return "ok";
  if (status === "degraded") return "warning";
  return "critical";
}

// Coarse "updated Xs/Xm ago" caption from the query's dataUpdatedAt.
function updatedAgo(updatedAt: number): string {
  const secs = Math.max(0, Math.round((Date.now() - updatedAt) / 1000));
  if (secs < 60) return `Updated ${secs}s ago`;
  return `Updated ${Math.round(secs / 60)}m ago`;
}

export function StatusPage() {
  const q = useClusterStatus();
  const data = q.data;
  const verdict = data ? clusterVerdict(data) : null;

  // The banner below is a role="status" region, but its text only changes on
  // the *count* of issues; a cluster that goes from fine to broken between two
  // 15s polls is otherwise silent. Announce the flip and nothing else — one
  // sentence when it breaks (assertive: this interrupts, it is why the page
  // exists) and one when it recovers (polite: good news can wait its turn).
  const announce = useAnnounce();
  const prevHealthyRef = useRef<boolean | null>(null);
  useEffect(() => {
    if (!verdict) return;
    const healthy = verdict.tone === "ok";
    const prev = prevHealthyRef.current;
    prevHealthyRef.current = healthy;
    if (prev === null || prev === healthy) return;
    announce(`Cluster status: ${verdict.label}.`, healthy ? undefined : { assertive: true });
  }, [verdict, announce]);

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <h1>Status</h1>
      </header>
      {q.isPending ? (
        <div className={styles.empty}>
          <Spinner size={20} />
        </div>
      ) : q.error || !data ? (
        <Card padded>
          <p className={styles.empty}>Cluster status not available.</p>
        </Card>
      ) : (
        <div className={styles.grid}>
          {verdict ? (
            <div
              className={`${styles.verdict} ${styles[verdict.tone]!} ${styles.full!}`}
              role="status"
            >
              <span className={styles.verdictLabel}>{verdict.label}</span>
              <span className={styles.updated}>{updatedAgo(q.dataUpdatedAt)}</span>
            </div>
          ) : null}
          <Card padded className={styles.full!}>
            <h2 className={styles.cardTitle}>Cluster</h2>
            {data.cluster?.members && data.cluster.members.length > 0 ? (
              <table className={styles.table}>
                <thead>
                  <tr>
                    <th>Member</th>
                    <th>Status</th>
                  </tr>
                </thead>
                <tbody>
                  {data.cluster.members.map((m) => (
                    <tr key={m.name}>
                      <td>
                        <Code>{m.name}</Code>
                        {data.cluster?.leader === m.name ? (
                          <Badge variant="info">leader</Badge>
                        ) : null}
                      </td>
                      <td>
                        <Badge variant={memberBadgeVariant(m.status)}>{m.status}</Badge>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : (
              <p className={styles.empty}>No members reported.</p>
            )}
          </Card>

          <Card padded className={styles.full!}>
            <h2 className={styles.cardTitle}>Plugins</h2>
            {(() => {
              const plugins = data.plugins ?? [];
              if (plugins.length === 0) return <p className={styles.empty}>No plugins reported.</p>;
              const loadedCount = plugins.filter((p) => p.loaded).length;
              const failed = plugins.filter((p) => !p.loaded);
              const allLoaded = failed.length === 0;
              const table = (
                <table className={styles.table}>
                  <thead>
                    <tr>
                      <th>Name</th>
                      <th>Loaded</th>
                    </tr>
                  </thead>
                  <tbody>
                    {plugins.map((p) => (
                      <tr key={p.name}>
                        <td>
                          <Code>{p.name}</Code>
                        </td>
                        <td>
                          <Badge variant={p.loaded ? "ok" : "muted"}>
                            {p.loaded ? "yes" : "no"}
                          </Badge>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              );
              // A wall of 40 "yes" rows says nothing an operator needs — fold
              // it behind one summary line. A failure is the one thing this
              // card exists to surface, so it stays expanded and loud instead
              // of hiding behind a click.
              if (allLoaded) {
                return (
                  <CollapsibleSection
                    title={`${loadedCount}/${plugins.length} plugins loaded`}
                    defaultOpen={false}
                  >
                    {table}
                  </CollapsibleSection>
                );
              }
              return (
                <>
                  <p className={styles.pluginFailure} role="alert">
                    {failed.length} of {plugins.length} plugins failed to load:{" "}
                    {failed.map((p) => p.name).join(", ")}
                  </p>
                  {table}
                </>
              );
            })()}
          </Card>
        </div>
      )}
    </div>
  );
}
