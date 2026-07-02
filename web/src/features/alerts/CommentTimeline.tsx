// CommentTimeline — renders the activity history of a single record:
//   - Inline composer for users with the can_comment permission.
//   - Per-type colored badge matching the legacy Vue palette:
//       comment → info (blue)   ack     → ok       (green)
//       esc     → warning (yel) close   → closed   (muted purple)
//       open    → neutral       shelve  → muted    unshelve → neutral
//   - Edit + delete affordances on the user's own comments, or for any
//     comment if the user holds rw_record / rw_all.
//   - Newest activity first (reverse-chronological): page 1 is the most
//     recent, the last page the oldest.
//   - Page controls (5 / 10 / 20 per page) with first/last page jumps.
import { useState } from "react";
import { Badge, type BadgeVariant } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { IconButton } from "@/shared/ui/IconButton";
import { Skeleton } from "@/shared/ui/Skeleton";
import { useAuth } from "@/lib/auth/store";
import { hasAnyPermission } from "@/lib/auth/permissions";
import { toast } from "@/shared/ui/toast/useToast";
import { ApiError } from "@/lib/api/client";
import { trimDate } from "./format";
import { Comments, useRecordComments, type Comment } from "./comments";
import { useCommentRecord } from "./api";
import { canTransition } from "./transitions";
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
  comment: "info", // blue
  ack: "ok", // green
  esc: "warning", // yellow
  close: "closed", // purple (muted)
  open: "neutral", // gray/blue
  shelve: "muted",
  unshelve: "neutral",
};

// The composer only offers free-form comment plus the two transitions that
// make sense to author with a note. All three are valid CommentInput types, so
// posting can route through useCommentRecord (which resyncs the record list).
const COMPOSER_TYPES = ["comment", "ack", "esc"] as const;
type ComposerType = (typeof COMPOSER_TYPES)[number];
const PAGE_SIZE_OPTIONS = [5, 10, 20] as const;

export function CommentTimeline({
  recordUid,
  state,
}: {
  recordUid: string | undefined;
  /**
   * The linked record's current lifecycle state. When provided, the composer
   * hides the ack/esc chips the backend would reject from that state (same
   * transition table as the rest of the Alerts UI). Omitted/undefined → all
   * chips shown (fail-open), matching the backend's permissive default.
   */
  state?: string | undefined;
}) {
  const { claims } = useAuth();
  const currentUser = claims?.sub ?? "";
  const canComment = hasAnyPermission(claims, ["can_comment"]);
  const canModerate = hasAnyPermission(claims, ["rw_record"]);

  const [pageSize, setPageSize] = useState<number>(5);
  const [page, setPage] = useState<number>(1);
  const q = useRecordComments(recordUid, {
    limit: pageSize,
    offset: (page - 1) * pageSize,
  });

  // Posting routes through useCommentRecord (not Comments.useCreate) so an
  // ack/esc authored here invalidates the record list/count too — not just the
  // comment log — keeping the alert table in sync with the state change.
  const post = useCommentRecord();
  const update = Comments.useUpdate();
  const remove = Comments.useRemove();

  // Composer state
  const [draft, setDraft] = useState("");
  const [draftType, setDraftType] = useState<ComposerType>("comment");
  // Edit state — one comment at a time.
  const [editingUid, setEditingUid] = useState<string | undefined>(undefined);
  const [editDraft, setEditDraft] = useState("");

  if (recordUid === undefined) {
    return <p className={styles.empty}>Open an alert to see its timeline.</p>;
  }

  const total = q.data?.meta.total ?? 0;
  const items = q.data?.data ?? [];
  const pageCount = Math.max(1, Math.ceil(total / pageSize));

  // Comment is always allowed; ack/esc only when the backend accepts them from
  // the current state. Without a known state, show all (fail-open).
  const composerTypes = COMPOSER_TYPES.filter(
    (t) => t === "comment" || state === undefined || canTransition(state, t),
  );

  async function handlePost() {
    if (!draft.trim() || !recordUid) return;
    try {
      await post.mutateAsync({
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
            <span className={styles.composerType} role="group" aria-label="Comment type">
              {composerTypes.map((t) => (
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
              loading={post.isPending}
              disabled={post.isPending || !draft.trim()}
              onClick={() => {
                void handlePost();
              }}
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
                        onClick={() => {
                          if (c.uid) void handleSaveEdit(c.uid);
                        }}
                      >
                        Save
                      </Button>
                    </span>
                  </>
                ) : (
                  <>
                    {c.message ? <p className={styles.message}>{c.message}</p> : null}
                    <span className={styles.meta}>
                      {c.user ?? "system"} · {trimDate(c.date_epoch)}
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
                aria-pressed={pageSize === n}
                onClick={() => {
                  setPageSize(n);
                  setPage(1);
                }}
              >
                {n}
              </button>
            ))}
            <IconButton
              icon="chevrons-left"
              label="First page"
              size="sm"
              variant="ghost"
              disabled={page <= 1}
              onClick={() => setPage(1)}
            />
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
            <IconButton
              icon="chevrons-right"
              label="Last page"
              size="sm"
              variant="ghost"
              disabled={page >= pageCount}
              onClick={() => setPage(pageCount)}
            />
          </span>
        </div>
      ) : null}
    </div>
  );
}
