// CommentTimeline — renders the activity history of a single record:
//   - Inline composer for users with the can_comment permission.
//   - Per-type colored badge matching the legacy Vue palette:
//       comment → info (blue)   ack     → ok       (green)
//       esc     → warning (yel) close   → muted    (gray)
//       open    → neutral       shelve  → muted    unshelve → neutral
//   - Edit + delete affordances on the user's own comments, or for any
//     comment if the user holds rw_record / rw_all.
//   - Page controls (5 / 10 / 20 per page).
import { useState } from "react";
import { Badge, type BadgeVariant } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { IconButton } from "@/shared/ui/IconButton";
import { Skeleton } from "@/shared/ui/Skeleton";
import { useAuth } from "@/lib/auth/store";
import { hasAnyPermission } from "@/lib/auth/permissions";
import { toast } from "@/shared/ui/toast/useToast";
import { ApiError } from "@/lib/api/client";
import { formatRelativeTime } from "./format";
import { Comments, useRecordComments, type Comment } from "./comments";
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

// Color palette: see web/src/utils/api.js:230-243 on origin/master for
// the legacy mapping. Mapped to the current Badge variants.
const TYPE_VARIANT: Record<Comment["type"], BadgeVariant> = {
  comment: "info",      // blue
  ack: "ok",            // green
  esc: "warning",       // yellow
  close: "muted",       // gray
  open: "neutral",      // gray/blue
  shelve: "muted",
  unshelve: "neutral",
};

const COMPOSER_TYPES: Comment["type"][] = ["comment", "ack", "esc"];
const PAGE_SIZE_OPTIONS = [5, 10, 20] as const;

export function CommentTimeline({ recordUid }: { recordUid: string | undefined }) {
  const { claims } = useAuth();
  const currentUser = (claims?.sub ?? "") as string;
  const canComment = hasAnyPermission(claims, ["can_comment"]);
  const canModerate = hasAnyPermission(claims, ["rw_record"]);

  const [pageSize, setPageSize] = useState<number>(5);
  const [page, setPage] = useState<number>(1);
  const q = useRecordComments(recordUid, {
    limit: pageSize,
    offset: (page - 1) * pageSize,
  });

  const create = Comments.useCreate();
  const update = Comments.useUpdate();
  const remove = Comments.useRemove();

  // Composer state
  const [draft, setDraft] = useState("");
  const [draftType, setDraftType] = useState<Comment["type"]>("comment");
  // Edit state — one comment at a time.
  const [editingUid, setEditingUid] = useState<string | undefined>(undefined);
  const [editDraft, setEditDraft] = useState("");

  if (recordUid === undefined) {
    return <p className={styles.empty}>Open an alert to see its timeline.</p>;
  }

  const total = q.data?.meta.total ?? 0;
  const items = q.data?.data ?? [];
  const pageCount = Math.max(1, Math.ceil(total / pageSize));

  async function handlePost() {
    if (!draft.trim() || !recordUid) return;
    try {
      await create.mutateAsync({
        record_uid: recordUid,
        type: draftType,
        message: draft.trim(),
      });
      setDraft("");
      setDraftType("comment");
      toast.success("Posted");
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : "Post failed");
    }
  }

  async function handleSaveEdit(uid: string) {
    if (!editDraft.trim()) return;
    try {
      await update.mutateAsync({ uid, body: { message: editDraft.trim() } });
      setEditingUid(undefined);
      setEditDraft("");
      toast.success("Saved");
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : "Save failed");
    }
  }

  async function handleDelete(uid: string) {
    try {
      await remove.mutateAsync(uid);
      toast.success("Deleted");
    } catch (e) {
      toast.error(e instanceof ApiError ? e.detail : "Delete failed");
    }
  }

  return (
    <div className={styles.timeline}>
      {/* Composer — gated on can_comment. */}
      {canComment ? (
        <div className={styles.composer}>
          <textarea
            className={styles.editArea}
            aria-label="New comment"
            rows={2}
            placeholder="Write a comment, ack, or escalation note…"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
          />
          <div className={styles.composerRow}>
            <span className={styles.composerType} role="radiogroup" aria-label="Comment type">
              {COMPOSER_TYPES.map((t) => (
                <button
                  key={t}
                  type="button"
                  className={styles.typeChip}
                  data-active={draftType === t || undefined}
                  aria-pressed={draftType === t}
                  onClick={() => setDraftType(t)}
                >
                  {TYPE_LABEL[t]}
                </button>
              ))}
            </span>
            <Button
              size="sm"
              variant="primary"
              loading={create.isPending}
              disabled={create.isPending || !draft.trim()}
              onClick={handlePost}
            >
              Post
            </Button>
          </div>
        </div>
      ) : (
        <p className={styles.gated}>You don't have permission to comment on this alert.</p>
      )}

      {/* List */}
      {q.isPending ? (
        Array.from({ length: pageSize }).map((_, i) => (
          <div key={i} className={styles.row}>
            <span className={styles.dot} />
            <Skeleton height={32} />
            <span />
          </div>
        ))
      ) : items.length === 0 ? (
        <p className={styles.empty}>No comments yet.</p>
      ) : (
        items.map((c) => {
          const isOwn = !!c.user && c.user === currentUser;
          const canEdit = isOwn || canModerate;
          return (
            <div key={c.uid ?? `${c.date_epoch}-${c.user ?? ""}`} className={styles.row}>
              <span className={styles.dot} />
              <div className={styles.body}>
                <span className={styles.head}>
                  <Badge variant={TYPE_VARIANT[c.type]}>{TYPE_LABEL[c.type]}</Badge>
                </span>
                {editingUid === c.uid ? (
                  <>
                    <textarea
                      className={styles.editArea}
                      rows={2}
                      value={editDraft}
                      onChange={(e) => setEditDraft(e.target.value)}
                      aria-label="Edit comment"
                    />
                    <span className={styles.composerRow}>
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => {
                          setEditingUid(undefined);
                          setEditDraft("");
                        }}
                      >
                        Cancel
                      </Button>
                      <Button
                        size="sm"
                        variant="primary"
                        loading={update.isPending}
                        disabled={!editDraft.trim() || update.isPending}
                        onClick={() => c.uid && handleSaveEdit(c.uid)}
                      >
                        Save
                      </Button>
                    </span>
                  </>
                ) : (
                  <>
                    {c.message ? <p className={styles.message}>{c.message}</p> : null}
                    <span className={styles.meta}>
                      {c.user ?? "system"} · {formatRelativeTime(c.date_epoch)}
                    </span>
                  </>
                )}
              </div>
              {canEdit && c.uid && editingUid !== c.uid ? (
                <span className={styles.actions}>
                  <IconButton
                    icon="edit"
                    label="Edit comment"
                    size="sm"
                    variant="ghost"
                    onClick={() => {
                      setEditingUid(c.uid);
                      setEditDraft(c.message ?? "");
                    }}
                  />
                  <IconButton
                    icon="trash"
                    label="Delete comment"
                    size="sm"
                    variant="ghost"
                    onClick={() => c.uid && void handleDelete(c.uid)}
                  />
                </span>
              ) : (
                <span />
              )}
            </div>
          );
        })
      )}

      {/* Pagination */}
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
                className={styles.typeChip}
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
