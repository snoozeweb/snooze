// ConditionPreview — collapsible card showing the first N alerts that
// match the supplied Condition. Mirrors the legacy Vue UI's affordance
// where editing a rule/snooze/notification shows a live preview of which
// alerts the filter currently catches.
import { useEffect, useMemo, useState } from "react";
import { Records } from "@/features/alerts/api";
import { Badge } from "@/shared/ui/Badge";
import { Icon } from "@/shared/icons/Icon";
import { encodeConditionQ } from "@/lib/condition/serialize";
import { severityBadgeVariant } from "@/features/alerts/format";
import type { Condition } from "@/lib/condition/types";
import styles from "./ConditionPreview.module.css";

export type ConditionPreviewProps = {
  condition: Condition | undefined;
  pageSize?: number;
};

const DEBOUNCE_MS = 300;

export function ConditionPreview({ condition, pageSize = 5 }: ConditionPreviewProps) {
  const [open, setOpen] = useState(false);
  const [page, setPage] = useState(1);

  // Debounce the condition prop so rapid keystrokes in leaf value inputs
  // (which update the AST per character) don't fire a filtered-list request
  // on every keystroke. Requests only go out after a DEBOUNCE_MS pause.
  const [debouncedCondition, setDebouncedCondition] = useState<Condition | undefined>(condition);
  useEffect(() => {
    const id = setTimeout(() => setDebouncedCondition(condition), DEBOUNCE_MS);
    return () => clearTimeout(id);
  }, [condition]);

  // The backend's record CRUD accepts a base64url-encoded condition AST
  // in `q`. ALWAYS_TRUE encodes as an empty list and matches everything.
  const q = useMemo(() => {
    if (!debouncedCondition) return undefined;
    try {
      return encodeConditionQ(debouncedCondition);
    } catch {
      return undefined;
    }
  }, [debouncedCondition]);

  const list = Records.useList({
    ...(q !== undefined ? { q } : {}),
    limit: pageSize,
    offset: (page - 1) * pageSize,
    orderby: "date_epoch",
    asc: false,
  });

  const total = list.data?.meta.total ?? 0;
  const items = list.data?.data ?? [];
  const pageCount = Math.max(1, Math.ceil(total / pageSize));

  return (
    <div className={styles.wrap}>
      <button
        type="button"
        className={styles.header}
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
      >
        <span className={styles.headerLeft}>
          <Icon name={open ? "chevron-down" : "chevron-right"} size={14} />
          Preview matching alerts
          {list.isPending ? (
            <span className={styles.muted}> (loading…)</span>
          ) : (
            <Badge variant="muted">{total}</Badge>
          )}
        </span>
      </button>
      {open ? (
        items.length === 0 ? (
          <div className={styles.empty}>No alerts match this condition yet.</div>
        ) : (
          <div className={styles.body}>
            {items.map((r) => (
              <div key={r.uid ?? `${r.host}-${r.date_epoch}`} className={styles.row}>
                <span>
                  {r.severity ? (
                    <Badge variant={severityBadgeVariant(r.severity)}>{r.severity}</Badge>
                  ) : (
                    <span className={styles.muted}>—</span>
                  )}
                </span>
                <span>{r.host ?? <span className={styles.muted}>(no host)</span>}</span>
                <span>{r.source ?? <span className={styles.muted}>—</span>}</span>
                <span className={styles.muted}>{r.state ?? "Open"}</span>
              </div>
            ))}
            <div className={styles.controls}>
              <span>
                Page {page} / {pageCount} · {total} total
              </span>
              <span className={styles.controlButtons}>
                <button
                  type="button"
                  className={styles.pageBtn}
                  disabled={page <= 1}
                  onClick={() => setPage((p) => Math.max(1, p - 1))}
                  aria-label="Previous page"
                >
                  <Icon name="chevron-left" size={14} />
                </button>
                <button
                  type="button"
                  className={styles.pageBtn}
                  disabled={page >= pageCount}
                  onClick={() => setPage((p) => Math.min(pageCount, p + 1))}
                  aria-label="Next page"
                >
                  <Icon name="chevron-right" size={14} />
                </button>
              </span>
            </div>
          </div>
        )
      ) : null}
    </div>
  );
}
