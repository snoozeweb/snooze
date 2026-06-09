import { Link } from "@tanstack/react-router";
import { Badge, type BadgeVariant } from "@/shared/ui/Badge";
import { Spinner } from "@/shared/ui/Spinner";
import { trimDate } from "@/features/alerts/format";
import type { Comment } from "@/features/alerts/comments";
import { useRecentActivity } from "./api";
import styles from "./ActivityFeed.module.css";

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
  comment: "info",
  ack: "ok",
  esc: "warning",
  close: "closed", // purple (muted)
  open: "neutral",
  shelve: "muted",
  unshelve: "neutral",
};

export function ActivityFeed() {
  const q = useRecentActivity(15);
  const items = q.data?.data ?? [];

  if (q.isPending) {
    return (
      <div className={styles.center}>
        <Spinner size={20} />
      </div>
    );
  }

  if (items.length === 0) {
    return <div className={styles.center}>No recent activity.</div>;
  }

  return (
    <ul className={styles.feed}>
      {items.map((c) => (
        <li key={c.uid ?? `${c.date_epoch ?? 0}-${c.user ?? ""}`} className={styles.row}>
          <Badge variant={TYPE_VARIANT[c.type]}>{TYPE_LABEL[c.type]}</Badge>
          <div className={styles.body}>
            <span className={styles.meta}>
              <strong>{c.user ?? "system"}</strong> · {trimDate(c.date_epoch)}
            </span>
            {c.message ? <span className={styles.message}>{c.message}</span> : null}
          </div>
          <Link
            className={styles.link}
            to="/web/alerts"
            search={{ search: `uid = "${c.record_uid}"` }}
            aria-label="Open alert"
          >
            ↗
          </Link>
        </li>
      ))}
    </ul>
  );
}
