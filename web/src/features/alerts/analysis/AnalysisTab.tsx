// The Analysis tab of the alert inspector: why the alert fired and what to do
// about it, as the alert-rca agent loop (or an operator) recorded it.
//
// One query drives this pane AND the tab's marker AND the inspector's one-line
// header pointer — they all call useAnalysis with the same uid, so TanStack
// Query serves the three readers from one request.
//
// The pane reads top to bottom in the order an on-call engineer needs it:
//
//   1. who wrote it and how far to trust it (provenance + confidence), and
//      whether it still describes this occurrence (the stale notice);
//   2. the verdict — act / watch / recovered, and the cause in one line;
//   3. what to do, with whether it can run unattended;
//   4. the caveats that qualify all of the above;
//   5. the evidence, folded: support, re-checked only when doubting.
//
// It used to run justification → evidence → plan → "Automatable: No", which
// put the one line a triager acts on under a paragraph and a list.
import { Suspense, lazy, useEffect, useState } from "react";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { ErrorBoundary } from "@/shared/ui/ErrorBoundary";
import { IconButton } from "@/shared/ui/IconButton";
import { InlineError } from "@/shared/ui/InlineError";
import { Menu, MenuContent, MenuItem, MenuTrigger } from "@/shared/ui/Menu";
import { Skeleton } from "@/shared/ui/Skeleton";
import { Icon } from "@/shared/icons/Icon";
import { describeError } from "@/lib/api/errorMessage";
import { useAnalysis } from "./api";
import { AnalysisEmpty } from "./AnalysisEmpty";
import { CaveatsCallout } from "./CaveatsCallout";
import { ClearAnalysisDialog } from "./ClearAnalysisDialog";
import { ConfidenceMeter } from "./ConfidenceMeter";
import { Disclosure } from "./Disclosure";
import { EvidenceList } from "./EvidenceList";
import { PlanView } from "./PlanView";
import { ProvenanceLine } from "./ProvenanceLine";
import { RootCauseView } from "./RootCauseView";
import { isConfidence } from "./enums";
import { useCanWriteAnalysis } from "./perms";
import { analysisIsStale, isPlanStatus, readCaveats } from "./verdict";
import styles from "./AnalysisTab.module.css";

/**
 * The editor drags in @dnd-kit (the step list's reorder), which is ~17 kB
 * gzipped that a reader never executes — and most people opening an alert are
 * reading. Splitting it out keeps the Alerts route's payload where it was
 * before this tab learned to write.
 */
const AnalysisEditor = lazy(async () => ({
  default: (await import("./AnalysisEditor")).AnalysisEditor,
}));

/** Read the stored analysis, or correct it in place. */
export type AnalysisMode = "view" | "edit";

function EditorSkeleton() {
  return (
    <div className={styles.loading} aria-busy="true">
      <Skeleton height={20} width="70%" />
      <Skeleton height={14} />
      <Skeleton height={14} width="85%" />
    </div>
  );
}

export type AnalysisTabProps = {
  /**
   * The alert this analysis hangs off. An alert row can legitimately have no
   * uid (see AlertsPage's `recordKey` fallback), and the route is addressed by
   * the parent record's uid — so no uid means no request and the empty state.
   */
  uid: string | undefined;
  /**
   * The alert's `date_epoch` — when it last fired. Compared with the
   * analysis's own timestamp to say when the alert has fired again since it
   * was written (the conclusion may describe an earlier occurrence). Absent
   * means "unknown", and no notice is shown.
   */
  lastEpoch?: number | undefined;
  /**
   * Told whenever the pane enters or leaves the editor (and `false` on
   * unmount). The surfaces *around* this one — the inspector's tab strip, the
   * drawer's prev/next — can destroy it with a single click, so they need to
   * know there is something to lose before they do.
   */
  onEditingChange?: ((editing: boolean) => void) | undefined;
};

/**
 * "This may be about an earlier occurrence." A notice, not an alarm — nothing
 * failed, the reader just needs to weigh the conclusion accordingly — so it
 * is neutral ink on the raised surface, with no role that interrupts.
 */
function StaleNotice() {
  return (
    <div className={styles.notice}>
      <Icon name="info" size={16} className={styles.noticeIcon!} />
      <p className={styles.noticeText}>
        This alert fired again after the analysis was written — it may describe an earlier
        occurrence.
      </p>
    </div>
  );
}

export function AnalysisTab({ uid, lastEpoch, onEditingChange }: AnalysisTabProps) {
  const [mode, setMode] = useState<AnalysisMode>("view");
  const [clearOpen, setClearOpen] = useState(false);
  const canWrite = useCanWriteAnalysis();
  const query = useAnalysis(uid, { enabled: uid !== undefined && uid !== "" });

  // Which alert the state below is about. `uid` is read fresh at submit time,
  // so an editor left open across a retarget would PUT the previous alert's
  // draft onto the new one. The drawer keys this subtree by row, which
  // remounts it; this is the same guarantee for every other caller (and for
  // the one render that happens before the remount).
  const [subject, setSubject] = useState(uid);
  const retargeted = subject !== uid;
  if (retargeted) {
    setSubject(uid);
    setMode("view");
    setClearOpen(false);
  }
  const effectiveMode: AnalysisMode = retargeted ? "view" : mode;
  const editing = effectiveMode === "edit";

  useEffect(() => {
    onEditingChange?.(editing);
    return () => {
      if (editing) onEditingChange?.(false);
    };
  }, [editing, onEditingChange]);

  // Writing is addressed by uid, so a row without one can only ever read.
  const writableUid = canWrite && uid !== undefined && uid !== "" ? uid : undefined;

  const agentic = query.data?.agentic;
  const rootCause = agentic?.root_cause;

  // A failed *refresh* — the request behind a pane that already has something
  // on it. It is reported beside the content, never instead of it: the pane
  // may be an open editor holding an operator's half-written correction, and
  // an error panel that replaces it destroys that work to say something the
  // operator can do nothing about.
  const refreshFailed = query.isError && query.data !== undefined;
  const refreshError = refreshFailed ? (
    <InlineError {...describeError(query.error, "Could not refresh the analysis.")} />
  ) : null;

  // Edit comes first — before loading, before the error panel, before the
  // empty state. Everything below this point is a *replacement* for the pane,
  // and nothing that happens in the background earns the right to replace an
  // open editor. ("Write analysis" from the empty state also lands here, with
  // `initial` null.)
  if (editing && writableUid !== undefined) {
    return (
      <div className={styles.pane}>
        <div className={styles.header}>
          <div className={styles.trust}>
            <Badge variant="neutral">Editing</Badge>
            <ProvenanceLine analysis={agentic?.analysis} />
          </div>
          {/* Edit / Remove are withheld while editing — they would act on the
              document being replaced. */}
          <div className={styles.actions} data-slot="analysis-actions" />
        </div>
        {refreshError}
        {/* The editor is a lazy chunk. After a deploy the chunk this bundle
            asks for is gone from the server, `import()` rejects, and without a
            boundary that rejection escapes to the router and takes the whole
            alerts route down — because a tab failed to load. */}
        <ErrorBoundary resetKey={writableUid} summary="The analysis editor could not be loaded.">
          <Suspense fallback={<EditorSkeleton />}>
            <AnalysisEditor
              uid={writableUid}
              initial={agentic ?? null}
              onSaved={() => setMode("view")}
              onCancel={() => setMode("view")}
            />
          </Suspense>
        </ErrorBoundary>
      </div>
    );
  }

  if (uid !== undefined && uid !== "" && query.isPending) {
    return (
      <div className={styles.loading} aria-busy="true">
        <Skeleton height={20} width="70%" />
        <Skeleton height={14} width="40%" />
        <Skeleton height={14} />
        <Skeleton height={14} width="85%" />
      </div>
    );
  }

  // A first load that failed has nothing to show beside the error, so here the
  // error IS the pane.
  if (query.isError && query.data === undefined) {
    return <InlineError {...describeError(query.error, "Could not load the analysis.")} />;
  }

  // A 404 arrives as `null` — "this alert carries no analysis" is the normal
  // state, not a failure (see api.ts). An envelope whose subtree lost its root
  // cause is equally nothing to show.
  if (!rootCause) {
    return (
      <>
        {refreshError}
        <AnalysisEmpty {...(writableUid !== undefined ? { onWrite: () => setMode("edit") } : {})} />
      </>
    );
  }

  // Guarded, not asserted: every one of these is whatever the stored
  // document carries, and a value this bundle does not know renders as
  // "undefined confidence" (or an unlabelled chip) if handed straight on.
  const confidence = isConfidence(rootCause.confidence) ? rootCause.confidence : undefined;
  const plan = agentic?.remediation_plan;
  const status = isPlanStatus(plan?.status) ? plan.status : undefined;
  const { evidence, caveats } = readCaveats(rootCause);
  const stale = analysisIsStale(lastEpoch, agentic?.analysis?.at);

  return (
    <div className={styles.pane}>
      {refreshError}
      {/* The byline, the freshness notice and the verdict are one block: the
          first two qualify the third, so they sit tight against it while the
          sections below are spaced apart. */}
      <div className={styles.lead}>
        <div className={styles.header}>
          <div className={styles.trust}>
            <ProvenanceLine analysis={agentic?.analysis} />
            {confidence !== undefined ? <ConfidenceMeter confidence={confidence} /> : null}
          </div>
          <div className={styles.actions} data-slot="analysis-actions">
            {writableUid !== undefined ? (
              <>
                <Button
                  size="sm"
                  variant="ghost"
                  leadingIcon="edit"
                  onClick={() => setMode("edit")}
                >
                  Edit
                </Button>
                {/* Remove is one deliberate step away, behind ⋯, and still
                    guarded by its dialog there. A red button beside the
                    confidence read as part of the verdict — and sat one
                    mis-click from Edit. */}
                <Menu>
                  <MenuTrigger>
                    <IconButton
                      icon="more-horizontal"
                      label="More analysis actions"
                      size="sm"
                      withTooltip={false}
                    />
                  </MenuTrigger>
                  <MenuContent>
                    <MenuItem danger leadingIcon="trash" onSelect={() => setClearOpen(true)}>
                      Remove
                    </MenuItem>
                  </MenuContent>
                </Menu>
              </>
            ) : null}
          </div>
        </div>
        {stale ? <StaleNotice /> : null}
        <RootCauseView rootCause={rootCause} status={status} showConfidence={false} />
      </div>
      <PlanView plan={plan} />
      <CaveatsCallout caveats={caveats} />
      {evidence.length > 0 ? (
        <Disclosure label={`Evidence · ${evidence.length}`}>
          <EvidenceList items={evidence} />
        </Disclosure>
      ) : null}
      {writableUid !== undefined ? (
        <ClearAnalysisDialog uid={writableUid} open={clearOpen} onOpenChange={setClearOpen} />
      ) : null}
    </div>
  );
}
