import { JsonViewer } from "@/shared/ui/JsonViewer";
import { CommentTimeline } from "./CommentTimeline";
import type { Record_ } from "./types";
import styles from "./AlertRowDetail.module.css";

export type AlertRowDetailProps = {
  row: Record_;
};

function stripPrivateKeys(row: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(row)) {
    if (k.startsWith("_")) continue;
    out[k] = v;
  }
  return out;
}

/**
 * AlertRowDetail — content for the inline row-expansion panel on the alerts
 * list. Mirrors RowDetailPanel's two-column layout (JsonViewer left, activity
 * right) but uses CommentTimeline instead of AuditTimeline because alerts
 * carry user-authored comments rather than CRUD audit events.
 */
export function AlertRowDetail({ row }: AlertRowDetailProps) {
  const cleaned = stripPrivateKeys(row as unknown as Record<string, unknown>);
  return (
    <div className={styles.grid}>
      <div className={styles.col}>
        <JsonViewer value={cleaned} />
      </div>
      <div className={styles.col}>
        <h4 className={styles.heading}>Timeline</h4>
        <CommentTimeline recordUid={row.uid} />
      </div>
    </div>
  );
}
