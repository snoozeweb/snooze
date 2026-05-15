import { JsonViewer } from "./JsonViewer";
import { AuditTimeline } from "@/features/audit/AuditTimeline";
import styles from "./RowDetailPanel.module.css";

export type RowDetailPanelProps = {
  row: Record<string, unknown>;
  objectType: string;
  objectId?: string | undefined;
};

function stripPrivateKeys(row: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(row)) {
    if (k.startsWith("_")) continue;
    out[k] = v;
  }
  return out;
}

export function RowDetailPanel({ row, objectType, objectId }: RowDetailPanelProps) {
  const cleaned = stripPrivateKeys(row);
  const uid = objectId ?? (typeof row.uid === "string" ? row.uid : undefined);
  return (
    <div className={styles.grid}>
      <div className={styles.col}>
        <JsonViewer value={cleaned} />
      </div>
      <div className={styles.col}>
        <h4 className={styles.heading}>Audit log</h4>
        <AuditTimeline objectType={objectType} objectId={uid} />
      </div>
    </div>
  );
}
