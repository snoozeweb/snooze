import { Card } from "@/shared/ui/Card";
import { Icon } from "@/shared/icons/Icon";
import type { Record_ } from "../types";
import styles from "./info.module.css";

type WithGrafana = Record_ & {
  image_url?: string;
  dashboard?: string;
  panel?: string;
  alert_state?: string;
};

export function GrafanaPane({ record }: { record: Record_ }) {
  const r = record as WithGrafana;
  if (!r.image_url) return null;

  return (
    <Card padded>
      <div className={styles.pane}>
        <h3 className={styles.title}>
          <Icon name="gauge" size={14} />
          Grafana
        </h3>
        <div className={styles.grid}>
          {r.dashboard ? (
            <>
              <span className={styles.label}>Dashboard</span>
              <span className={styles.value}>{r.dashboard}</span>
            </>
          ) : null}
          {r.panel ? (
            <>
              <span className={styles.label}>Panel</span>
              <span className={styles.value}>{r.panel}</span>
            </>
          ) : null}
          {r.alert_state ? (
            <>
              <span className={styles.label}>State</span>
              <span className={styles.value}>{r.alert_state}</span>
            </>
          ) : null}
        </div>
        <img className={styles.image} src={r.image_url} alt="Grafana panel screenshot" />
      </div>
    </Card>
  );
}
