// The Actions tab's row inspector — the same shell as `NotificationDetail`,
// scoped to one action.
//
// An action is the HOW: which notifier, configured with what, batched or not.
// Its Deliveries tab answers "is this integration actually working?", which is
// the question that sends people to the Actions tab in the first place.
import { Badge } from "@/shared/ui/Badge";
import { Code } from "@/shared/ui/Code";
import { EmptyState } from "@/shared/ui/EmptyState";
import { summarizeSubcontent } from "./columns";
import { DetailShell } from "./NotificationDetail";
import type { DeliveryFilter, DeliveryRange } from "./deliveries/types";
import type { Action } from "./types";
import styles from "./NotificationDetail.module.css";

export type ActionDetailProps = {
  row: Action;
  initialRange?: DeliveryRange | undefined;
  onRangeClear?: (() => void) | undefined;
};

export function ActionDetail({ row, initialRange, onRangeClear }: ActionDetailProps) {
  const name = row.name;
  const notifier = row.action?.selected;
  const batched = row.action?.subcontent?.["batch"] === true;
  const hint = summarizeSubcontent(row.action?.subcontent);

  const summary = (
    <div className={styles.summary}>
      <div className={styles.identity}>
        <Code>{name}</Code>
        <Badge variant="neutral">{notifier ?? "—"}</Badge>
      </div>
      {hint ? <p className={styles.condition}>{hint}</p> : null}
      <p className={styles.meta}>
        <span className={styles.metaLabel}>Batch</span>
        <Badge variant={batched ? "info" : "muted"}>{batched ? "yes" : "no"}</Badge>
      </p>
    </div>
  );

  return (
    <DetailShell
      summary={summary}
      // The log keys deliveries by action NAME, not uid (an action is
      // referenced by name from every notification), so a row only needs a
      // name to have a history.
      {...(name ? { filter: { kind: "action", name } as DeliveryFilter } : {})}
      variant="action"
      emptyState={
        <EmptyState
          icon="bell"
          title="No deliveries yet"
          description="Rows appear here once a notification routes an alert to this action."
        />
      }
      {...(initialRange ? { initialRange } : {})}
      {...(onRangeClear ? { onRangeClear } : {})}
      row={row as unknown as Record<string, unknown>}
      objectType="action"
      objectId={row.uid}
    />
  );
}
