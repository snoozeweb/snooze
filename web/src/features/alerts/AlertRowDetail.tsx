import { JsonViewer } from "@/shared/ui/JsonViewer";
import { Tabs, TabList, TabTrigger, TabPanel } from "@/shared/ui/Tabs";
import { Badge } from "@/shared/ui/Badge";
import { TimeCell } from "@/shared/ui/TimeCell";
import { severityColor } from "@/lib/format/severity-color";
import { stateBadgeVariant, stateLabel } from "./format";
import { CommentTimeline } from "./CommentTimeline";
import { AlertFlowChart } from "./AlertFlowChart";
import type { AlertState, Record_ } from "./types";
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
 * AlertRowDetail — the body of the docked row inspector on the alerts list.
 *
 * A compact summary header (severity + state badges, source chip, the alert
 * message, received time) sits above three tabs:
 *   - Timeline (default): comment/activity history + composer — the read-write
 *     action surface, given top billing since triage lives here.
 *   - Flow: the pipeline path the alert took (AlertFlowChart) — read-only.
 *   - Record: the raw record (JsonViewer) — read-only reference.
 *
 * The host is intentionally NOT repeated here — it is the inspector's title.
 * One layout for every viewport: the panel is already narrow, so the old
 * desktop 3-column grid (and its useIsMobileShell fork) is gone. Radix Tabs
 * mounts only the active panel, so CommentTimeline is instantiated once and its
 * comment fetch fires once.
 */
export function AlertRowDetail({ row }: AlertRowDetailProps) {
  const cleaned = stripPrivateKeys(row as unknown as Record<string, unknown>);
  const state = (row.state ?? "") as AlertState;

  return (
    <div className={styles.detail}>
      <div className={styles.summary}>
        <div className={styles.badges}>
          <Badge color={severityColor(row.severity ?? "")}>{row.severity ?? "—"}</Badge>
          <Badge variant={stateBadgeVariant(state)}>{stateLabel(state)}</Badge>
          {row.source ? <span className={styles.source}>{row.source}</span> : null}
        </div>
        {row.message ? <p className={styles.message}>{row.message}</p> : null}
        <div className={styles.received}>
          <TimeCell epoch={row.date_epoch} />
        </div>
      </div>

      <Tabs defaultValue="timeline">
        <TabList>
          <TabTrigger value="timeline">Timeline</TabTrigger>
          <TabTrigger value="flow">Flow</TabTrigger>
          <TabTrigger value="record">Record</TabTrigger>
        </TabList>
        <TabPanel value="timeline">
          <CommentTimeline recordUid={row.uid} state={row.state} />
        </TabPanel>
        <TabPanel value="flow">
          <AlertFlowChart row={row} />
        </TabPanel>
        <TabPanel value="record">
          <JsonViewer value={cleaned} />
        </TabPanel>
      </Tabs>
    </div>
  );
}
