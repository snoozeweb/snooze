import { JsonViewer } from "@/shared/ui/JsonViewer";
import { Tabs, TabList, TabTrigger, TabPanel } from "@/shared/ui/Tabs";
import { useIsMobileShell } from "@/shared/hooks/useIsMobileShell";
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
 * list. Three surfaces:
 *   - Record: the raw record (JsonViewer) — read-only reference.
 *   - Flow: the pipeline path the alert took (AlertFlowChart) — read-only.
 *   - Timeline: comment/activity history + composer (CommentTimeline) — the
 *     read-write action surface.
 *
 * On desktop all three show at once in a 50/25/25 grid (Record · Flow ·
 * Timeline) — no tabs, no headings; each panel is self-labelling. Below the
 * app shell's mobile breakpoint they collapse into tabs (Timeline default) so
 * a phone shows one surface at a time instead of an endless scroll.
 *
 * The desktop-vs-tabs choice is a JS switch (useIsMobileShell) rather than a
 * CSS show/hide so CommentTimeline mounts once — a CSS toggle would double it
 * and fire its comment fetch twice.
 */
export function AlertRowDetail({ row }: AlertRowDetailProps) {
  const cleaned = stripPrivateKeys(row as unknown as Record<string, unknown>);
  const isMobile = useIsMobileShell();

  const record = <JsonViewer value={cleaned} />;
  const flow = <AlertFlowChart row={row} />;
  const timeline = <CommentTimeline recordUid={row.uid} state={row.state} />;

  if (isMobile) {
    return (
      <Tabs defaultValue="timeline">
        <TabList>
          <TabTrigger value="timeline">Timeline</TabTrigger>
          <TabTrigger value="flow">Flow</TabTrigger>
          <TabTrigger value="record">Record</TabTrigger>
        </TabList>
        <TabPanel value="timeline">{timeline}</TabPanel>
        <TabPanel value="flow">{flow}</TabPanel>
        <TabPanel value="record">{record}</TabPanel>
      </Tabs>
    );
  }

  return (
    <div className={styles.grid}>
      <div className={styles.col}>{record}</div>
      <div className={styles.col}>{flow}</div>
      <div className={styles.col}>{timeline}</div>
    </div>
  );
}
