import { useState } from "react";
import { Badge } from "@/shared/ui/Badge";
import type { BadgeVariant } from "@/shared/ui/Badge";
import { IconButton } from "@/shared/ui/IconButton";
import { Skeleton } from "@/shared/ui/Skeleton";
import { formatRelativeTime } from "@/features/alerts/format";
import { useObjectAudit } from "./api";
import type { AuditAction } from "./types";
import styles from "./AuditTimeline.module.css";

const ACTION_LABEL: Record<AuditAction, string> = {
  create: "created",
  patch: "edited",
  replace: "replaced",
  delete: "deleted",
};

const ACTION_VARIANT: Record<AuditAction, BadgeVariant> = {
  create: "info",
  patch: "neutral",
  replace: "warning",
  delete: "muted",
};

export type AuditTimelineProps = {
  objectType: string;
  objectId: string | undefined;
};

const PAGE_SIZE_OPTIONS = [5, 10, 20] as const;

export function AuditTimeline({ objectType, objectId }: AuditTimelineProps) {
  const [pageSize, setPageSize] = useState<number>(5);
  const [page, setPage] = useState<number>(1);
  const q = useObjectAudit(objectType, objectId, {
    limit: pageSize,
    offset: (page - 1) * pageSize,
  });

  if (objectId === undefined) {
    // Editor in create mode — there's no history yet.
    return <p className={styles.empty}>Audit log appears here once the {objectType} is saved.</p>;
  }
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
  const total = q.data?.meta.total ?? 0;
  const pageCount = Math.max(1, Math.ceil(total / pageSize));

  if (items.length === 0) {
    return <p className={styles.empty}>No changes recorded yet.</p>;
  }
  return (
    <div className={styles.timeline}>
      {items.map((c) => (
        <div key={c.uid ?? `${c.date_epoch}-${c.action}`} className={styles.row}>
          <span className={styles.dot} />
          <div className={styles.body}>
            <span className={styles.head}>
              <Badge variant={ACTION_VARIANT[c.action]}>{ACTION_LABEL[c.action]}</Badge>
              {c.summary ? <span className={styles.fields}>{c.summary}</span> : null}
            </span>
            <span className={styles.meta}>
              {c.username ? c.username : "system"}
              {c.method ? ` (${c.method})` : ""}
              {" · "}
              {formatRelativeTime(c.date_epoch)}
            </span>
          </div>
        </div>
      ))}
      {total > pageSize ? (
        <div className={styles.controls}>
          <span>
            Page {page} / {pageCount} · {total} total
          </span>
          <span className={styles.controlButtons}>
            {PAGE_SIZE_OPTIONS.map((n) => (
              <button
                key={n}
                type="button"
                className={styles.sizeChip}
                data-active={pageSize === n || undefined}
                onClick={() => {
                  setPageSize(n);
                  setPage(1);
                }}
              >
                {n}
              </button>
            ))}
            <IconButton
              icon="chevron-left"
              label="Previous page"
              size="sm"
              variant="ghost"
              disabled={page <= 1}
              onClick={() => setPage((p) => Math.max(1, p - 1))}
            />
            <IconButton
              icon="chevron-right"
              label="Next page"
              size="sm"
              variant="ghost"
              disabled={page >= pageCount}
              onClick={() => setPage((p) => Math.min(pageCount, p + 1))}
            />
          </span>
        </div>
      ) : null}
    </div>
  );
}
