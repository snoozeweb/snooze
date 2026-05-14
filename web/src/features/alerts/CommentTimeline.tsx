import { Badge } from "@/shared/ui/Badge";
import type { BadgeVariant } from "@/shared/ui/Badge";
import { Skeleton } from "@/shared/ui/Skeleton";
import { formatRelativeTime } from "./format";
import { useRecordComments, type Comment } from "./comments";
import styles from "./CommentTimeline.module.css";

const TYPE_LABEL: Record<Comment["type"], string> = {
  comment: "commented",
  ack: "acknowledged",
  close: "closed",
  open: "re-opened",
  esc: "re-escalated",
  shelve: "shelved",
  unshelve: "unshelved",
};

const TYPE_VARIANT: Record<Comment["type"], BadgeVariant> = {
  comment: "neutral",
  ack: "info",
  close: "muted",
  open: "info",
  esc: "warning",
  shelve: "muted",
  unshelve: "neutral",
};

export function CommentTimeline({ recordUid }: { recordUid: string | undefined }) {
  const q = useRecordComments(recordUid);

  if (q.isPending) {
    return (
      <div className={styles.timeline} aria-busy="true">
        {Array.from({ length: 3 }).map((_, i) => (
          <div key={i} className={styles.row}>
            <span className={styles.dot} />
            <Skeleton height={32} />
          </div>
        ))}
      </div>
    );
  }
  const items = q.data?.data ?? [];
  if (items.length === 0) {
    return <p className={styles.empty}>No comments yet.</p>;
  }
  return (
    <div className={styles.timeline}>
      {items.map((c) => (
        <div key={c.uid ?? `${c.date_epoch}-${c.user ?? ""}`} className={styles.row}>
          <span className={styles.dot} />
          <div className={styles.body}>
            <span className={styles.head}>
              <Badge variant={TYPE_VARIANT[c.type]}>{TYPE_LABEL[c.type]}</Badge>
            </span>
            {c.message ? <p className={styles.message}>{c.message}</p> : null}
            <span className={styles.meta}>
              {c.user ?? "system"} · {formatRelativeTime(c.date_epoch)}
            </span>
          </div>
        </div>
      ))}
    </div>
  );
}
