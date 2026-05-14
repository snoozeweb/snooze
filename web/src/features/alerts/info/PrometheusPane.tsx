import { Card } from "@/shared/ui/Card";
import { Icon } from "@/shared/icons/Icon";
import type { Record_ } from "../types";
import styles from "./info.module.css";

type WithPrometheus = Record_ & {
  prometheus?: {
    alertname?: string;
    instance?: string;
    summary?: string;
    description?: string;
    [key: string]: unknown;
  };
};

export function PrometheusPane({ record }: { record: Record_ }) {
  const p = (record as WithPrometheus).prometheus;
  if (!p || (typeof p === "object" && Object.keys(p).length === 0)) return null;

  return (
    <Card padded>
      <div className={styles.pane}>
        <h3 className={styles.title}>
          <Icon name="activity" size={14} />
          Prometheus
        </h3>
        <div className={styles.grid}>
          {p.alertname ? (
            <>
              <span className={styles.label}>Alert</span>
              <span className={styles.value}>{p.alertname}</span>
            </>
          ) : null}
          {p.instance ? (
            <>
              <span className={styles.label}>Instance</span>
              <span className={styles.value}>{p.instance}</span>
            </>
          ) : null}
          {p.summary ? (
            <>
              <span className={styles.label}>Summary</span>
              <span className={styles.value}>{p.summary}</span>
            </>
          ) : null}
          {p.description ? (
            <>
              <span className={styles.label}>Description</span>
              <span className={styles.value}>{p.description}</span>
            </>
          ) : null}
        </div>
      </div>
    </Card>
  );
}
