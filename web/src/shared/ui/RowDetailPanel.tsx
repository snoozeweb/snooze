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

/** The raw-record half of the inspector: the "Record" heading + JSON viewer.
 *  Exported so richer inspectors (e.g. the notifications page's tabbed
 *  `NotificationDetail`) can host it as one tab instead of duplicating it. */
export function RecordSection({ row }: { row: Record<string, unknown> }) {
  return (
    <section>
      <h4 className={styles.heading}>Record</h4>
      <JsonViewer value={stripPrivateKeys(row)} />
    </section>
  );
}

/** The audit half of the inspector: the "Audit log" heading + timeline.
 *  Exported alongside `RecordSection` for the same reason. */
export function AuditSection({
  objectType,
  objectId,
}: {
  objectType: string;
  objectId?: string | undefined;
}) {
  return (
    <section>
      <h4 className={styles.heading}>Audit log</h4>
      <AuditTimeline objectType={objectType} objectId={objectId} />
    </section>
  );
}

export function RowDetailPanel({ row, objectType, objectId }: RowDetailPanelProps) {
  const uid = objectId ?? (typeof row.uid === "string" ? row.uid : undefined);
  // Rendered inside the docked row inspector (~600px), so the record and its
  // audit log stack vertically rather than sitting side-by-side.
  return (
    <div className={styles.stack}>
      <RecordSection row={row} />
      <AuditSection objectType={objectType} objectId={uid} />
    </div>
  );
}
