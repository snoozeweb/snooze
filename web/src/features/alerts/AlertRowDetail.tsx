import { JsonViewer } from "@/shared/ui/JsonViewer";
import { Tabs, TabList, TabTrigger, TabPanel } from "@/shared/ui/Tabs";
import { CommentTimeline } from "./CommentTimeline";
import { AlertFlowChart } from "./AlertFlowChart";
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
 * list. Left column: the raw record (JsonViewer). Right column: a tabbed
 * container — Timeline (comment/activity history, default) and Flow (the
 * pipeline path the alert took).
 */
export function AlertRowDetail({ row }: AlertRowDetailProps) {
  const cleaned = stripPrivateKeys(row as unknown as Record<string, unknown>);
  return (
    <div className={styles.grid}>
      <div className={styles.col}>
        <JsonViewer value={cleaned} />
      </div>
      <div className={styles.col}>
        <Tabs defaultValue="timeline">
          <TabList>
            <TabTrigger value="timeline">Timeline</TabTrigger>
            <TabTrigger value="flow">Flow</TabTrigger>
          </TabList>
          <TabPanel value="timeline">
            <CommentTimeline recordUid={row.uid} state={row.state} />
          </TabPanel>
          <TabPanel value="flow">
            <AlertFlowChart row={row} />
          </TabPanel>
        </Tabs>
      </div>
    </div>
  );
}
