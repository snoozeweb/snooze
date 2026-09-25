// CommentTimeline — renders the activity history of a single record:
//   - Inline composer for users with the can_comment permission.
//   - Per-type colored badge matching the legacy Vue palette:
//       comment → info (blue)   ack     → ack      (violet)
//       esc     → warning (yel) close   → closed   (muted purple)
//       open    → neutral       shelve  → muted    unshelve → neutral
//     plus the two ownership types: assign → ack (violet: someone has it
//     now, the same hue as an ack) and release → neutral (like a re-open).
//   - The author's face in the gutter; a bot glyph for auto/system entries.
//     An `assign` entry also names who it was handed to.
//   - Edit + delete affordances on the user's own comments, or for any
//     comment if the user holds rw_record / rw_all.
//   - Newest activity first (reverse-chronological): page 1 is the most
//     recent, the last page the oldest.
//   - Automatic repeats folded: a run of identical auto-comments ("New
//     escalation" every quarter of an hour for weeks) is one entry with its
//     count, cadence and start, expandable to its timestamps. What people
//     wrote is never folded, so it cannot be buried under the bot.
//   - Page controls (5 / 10 / 20 per page) with first/last page jumps.
import { useState } from "react";
import { Badge, type BadgeVariant } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogTitle } from "@/shared/ui/Dialog";
import { IconButton } from "@/shared/ui/IconButton";
import { Skeleton } from "@/shared/ui/Skeleton";
import { Avatar } from "@/shared/ui/Avatar";
import { personLabel, usePerson } from "@/shared/people/api";
import { useAuth } from "@/lib/auth/store";
import { hasAnyPermission } from "@/lib/auth/permissions";
import { toast } from "@/shared/ui/toast/useToast";
import { ApiError } from "@/lib/api/client";
import { trimDate } from "./format";
import {
  Comments,
  useCommentsBetween,
  useRecordCommentRuns,
  type Comment,
  type CommentRun,
} from "./comments";
import { cadenceLabel, repeatLabel } from "@/lib/format/cadence";
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
  assign: "assigned",
  release: "released",
};

// Color palette: see web/src/utils/api.js:230-243 on origin/master for
// the legacy mapping. Mapped to the current Badge variants.
const TYPE_VARIANT: Record<Comment["type"], BadgeVariant> = {
  comment: "info", // blue
  ack: "ack", // violet — a human has it
  esc: "warning", // gold
  close: "closed", // sage, muted — done and inert
  open: "neutral", // gray
  shelve: "muted",
  unshelve: "neutral",
  assign: "ack", // violet — somebody has it now
  release: "neutral",
};

/** "to <face> Alice Martin" under an `assign` entry. */
function AssigneeLine({ name, method }: { name: string; method?: string | undefined }) {
  const person = usePerson(name, method);
  return (
    <p className={styles.assignee}>
      <span>to</span>
      <Avatar name={name} method={method} size="sm" decorative />
      <span className={styles.assigneeName}>{personLabel(person, name)}</span>
    </p>
  );
}

// The composer only offers free-form comment plus the two transitions that
// make sense to author with a note. All three are valid CommentInput types, so
// posting can route through useCommentRecord (which resyncs the record list).
const COMPOSER_TYPES = ["comment", "ack", "esc"] as const;
type ComposerType = (typeof COMPOSER_TYPES)[number];
// The submit button names exactly what it will do — a generic "Post" that
// silently changes alert state is a doomed affordance.
const SUBMIT_LABEL: Record<ComposerType, string> = {
  comment: "Comment",
  ack: "Acknowledge",
  esc: "Re-escalate",
};
const PAGE_SIZE_OPTIONS = [5, 10, 20] as const;

/** Timestamps fetched per "Show older" step of an expanded run. */
const RUN_MEMBER_STEP = 50;

/**
 * "×2,100 · every ~16 min · since Sep 9th 12:14   Show all" under a folded
 * entry, and on demand the timestamps of every member — the only thing that
 * differs between them.
 */
function RunLine({ run, recordUid }: { run: CommentRun; recordUid: string }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <p className={styles.runLine}>
        <span className={styles.runCount}>{repeatLabel(run.count)}</span>
        <span>
          {[
            cadenceLabel(run.interval_s),
            `${run.truncated ? "since at least" : "since"} ${trimDate(run.first_epoch)}`,
          ]
            .filter(Boolean)
            .join(" · ")}
        </span>
        <Button
          size="sm"
          variant="ghost"
          className={styles.runToggle}
          aria-expanded={open}
          trailingIcon={open ? "chevron-up" : "chevron-down"}
          onClick={() => setOpen((v) => !v)}
        >
          {open ? "Hide" : `Show all ${run.count.toLocaleString("en-US")}`}
        </Button>
      </p>
      {open ? <RunTimes run={run} recordUid={recordUid} /> : null}
    </>
  );
}

function RunTimes({ run, recordUid }: { run: CommentRun; recordUid: string }) {
  const [shown, setShown] = useState(RUN_MEMBER_STEP);
  const q = useCommentsBetween(recordUid, { from: run.first_epoch, to: run.last_epoch }, shown);
  if (q.isPending) return <Skeleton height={24} />;
  if (q.isError) return <p className={styles.empty}>Could not load these entries.</p>;
  // The window can also hold a person's comment written in the same second
  // as a boundary member; only the run's own kind of entry is listed.
  const members = q.data.data.filter(
    (c) => c.auto === true && c.type === run.latest.type && c.message === run.latest.message,
  );
  return (
    <div className={styles.runTimes}>
      <ul className={styles.runTimeList} aria-label="Every occurrence in this run">
        {/* Plain dates, not TimeCell: its "Nm ago" prefix doubles the width
            of a cell in a list that is nothing but dates. */}
        {members.map((c) => (
          <li key={c.uid ?? c.date_epoch}>
            <time dateTime={new Date((c.date_epoch ?? 0) * 1000).toISOString()}>
              {trimDate(c.date_epoch)}
            </time>
          </li>
        ))}
      </ul>
      {q.data.meta.total > q.data.data.length ? (
        <Button
          size="sm"
          variant="ghost"
          className={styles.runToggle}
          loading={q.isFetching}
          onClick={() => setShown((n) => n + RUN_MEMBER_STEP)}
        >
          Show older
        </Button>
      ) : null}
    </div>
  );
}

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
  const q = useRecordCommentRuns(recordUid, {
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
  // Delete confirmation — comment deletion is irreversible, so it goes behind a
  // confirm dialog instead of firing on a single trash-icon click.
  const [deletingUid, setDeletingUid] = useState<string | undefined>(undefined);

  if (recordUid === undefined) {
    return <p className={styles.empty}>Open an alert to see its timeline.</p>;
  }

  // Pages walk the runs; the entry count is what the timeline holds.
  const total = q.data?.meta.total ?? 0;
  const entryCount = q.data?.meta.comments ?? 0;
  const runs = q.data?.data ?? [];
  const pageCount = Math.max(1, Math.ceil(total / pageSize));

  // Comment is always allowed; ack/esc only when the backend accepts them from
  // the current state. Without a known state, show all (fail-open).
  const composerTypes = COMPOSER_TYPES.filter(
    (t) => t === "comment" || state === undefined || canTransition(state, t),
  );

  async function handlePost() {
    const rid = recordUid;
    if (!draft.trim() || !rid) return;
    const type = draftType;
    try {
      await post.mutateAsync({ record_uid: rid, type, message: draft.trim() });
      setDraft("");
      setDraftType("comment");
      if (type === "ack" || type === "esc") {
        // A state change from here is undoable, like the inline row actions: the
        // Undo posts a compensating re-open (both events stay on the timeline).
        const verb = type === "ack" ? "Acknowledged" : "Re-escalated";
        toast.undo(verb, () => {
          void (async () => {
            try {
              await post.mutateAsync({ record_uid: rid, type: "open" });
            } catch (e) {
              toast.error(e instanceof ApiError ? e.detail : "Undo failed");
            }
          })();
        });
      } else {
        toast.success("Comment posted");
      }
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
      setDeletingUid(undefined);
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
            placeholder="Write a comment, acknowledgement, or re-escalation note…"
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
              {SUBMIT_LABEL[draftType]}
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
      ) : runs.length === 0 ? (
        <p className={styles.empty}>No comments yet.</p>
      ) : (
        runs.map((run) => {
          const c = run.latest;
          // Read auto defensively: the OpenAPI schema does not yet describe this field.
          // Auto-comments (from the housekeeper or aggregaterule plugin) are attributed
          // as "System (auto)" and cannot be edited or deleted.
          const isAuto = (c as { auto?: unknown }).auto === true;
          // A tool acting on the user's behalf is named beside them, so a person
          // and an agent sharing one account read differently.
          const author = isAuto ? "System (auto)" : (c.user ?? "system");
          const attribution = c.source ? `${author} via ${c.source}` : author;
          const isOwn = !!c.user && c.user === currentUser;
          const canEdit = !isAuto && (isOwn || canModerate);
          return (
            <div
              key={run.key || c.uid || `${c.date_epoch}-${c.user ?? ""}`}
              className={styles.row}
              data-auto={isAuto || undefined}
              data-run={run.count > 1 || undefined}
            >
              {/* The author's face, where the bare dot used to be; the name is
                  in the meta line beside the badge, so the face is decoration. */}
              <span className={styles.gutter}>
                {isAuto || !c.user ? (
                  <Avatar name="" variant="bot" decorative />
                ) : (
                  <Avatar name={c.user} method={c.method} decorative />
                )}
              </span>
              <div className={styles.body}>
                {/* Badge and its who·when meta share one line — the timestamp
                    sits flush-right of the badge — so each entry stays compact
                    in the narrow desktop Timeline column. */}
                <span className={styles.head}>
                  <Badge variant={TYPE_VARIANT[c.type]}>{TYPE_LABEL[c.type]}</Badge>
                  <span className={styles.meta}>
                    {attribution} · {trimDate(c.date_epoch)}
                  </span>
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
                    {c.type === "assign" && c.assignee ? (
                      <AssigneeLine name={c.assignee} method={c.assignee_method} />
                    ) : null}
                    {c.message ? <p className={styles.message}>{c.message}</p> : null}
                    {run.count > 1 ? <RunLine run={run} recordUid={recordUid} /> : null}
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
                    variant="ghostDanger"
                    onClick={() => c.uid && setDeletingUid(c.uid)}
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
            Page {page} / {pageCount} · {entryCount} {entryCount === 1 ? "entry" : "entries"}
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

      <Dialog
        open={deletingUid !== undefined}
        onOpenChange={(o) => {
          if (!o) setDeletingUid(undefined);
        }}
      >
        <DialogContent>
          <DialogTitle>Delete comment?</DialogTitle>
          <DialogBody>This permanently removes the comment. It cannot be undone.</DialogBody>
          <DialogFooter>
            <Button
              variant="secondary"
              onClick={() => setDeletingUid(undefined)}
              disabled={remove.isPending}
            >
              Cancel
            </Button>
            <Button
              variant="danger"
              loading={remove.isPending}
              disabled={remove.isPending}
              onClick={() => {
                if (deletingUid) void handleDelete(deletingUid);
              }}
            >
              Delete
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
