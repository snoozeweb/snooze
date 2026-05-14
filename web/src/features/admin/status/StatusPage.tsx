import { Badge } from "@/shared/ui/Badge";
import { Card } from "@/shared/ui/Card";
import { Code } from "@/shared/ui/Code";
import { Spinner } from "@/shared/ui/Spinner";
import { useClusterStatus } from "./api";
import type { ClusterMember } from "./types";
import styles from "./StatusPage.module.css";

function memberBadgeVariant(status: ClusterMember["status"]): "ok" | "warning" | "critical" {
  if (status === "ok") return "ok";
  if (status === "degraded") return "warning";
  return "critical";
}

export function StatusPage() {
  const q = useClusterStatus();
  const data = q.data;

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
            {data.plugins && data.plugins.length > 0 ? (
              <table className={styles.table}>
                <thead>
                  <tr>
                    <th>Name</th>
                    <th>Loaded</th>
                  </tr>
                </thead>
                <tbody>
                  {data.plugins.map((p) => (
                    <tr key={p.name}>
                      <td>
                        <Code>{p.name}</Code>
                      </td>
                      <td>
                        <Badge variant={p.loaded ? "ok" : "muted"}>{p.loaded ? "yes" : "no"}</Badge>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : (
              <p className={styles.empty}>No plugins reported.</p>
            )}
          </Card>
        </div>
      )}
    </div>
  );
}
