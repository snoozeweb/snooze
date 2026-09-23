// The ordinary state of almost every alert: nobody has worked out why it
// fired yet.
//
// It is an empty state rather than an error (the route answers 404 for "no
// analysis"), and it says where analyses come from — an operator who has never
// seen one otherwise has no way to know the tab is ever filled. In plain
// words: the permission's wire name (`rw_protected`) is the server's business,
// and printing it here told a reader nothing they could act on.
import { Button } from "@/shared/ui/Button";
import { EmptyState } from "@/shared/ui/EmptyState";
import { useCanWriteAnalysis } from "./perms";
import styles from "./AnalysisEmpty.module.css";

export type AnalysisEmptyProps = {
  /**
   * Start authoring an analysis. The button only appears when the caller
   * passes a handler AND the session holds the literal `rw_protected`
   * permission the server demands — offering it to anyone else is an invitation
   * to fill a form and then eat a 403.
   */
  onWrite?: () => void;
};

export function AnalysisEmpty({ onWrite }: AnalysisEmptyProps) {
  const canWrite = useCanWriteAnalysis();
  // Why there is no button, for the reader who looks for one. Only when the
  // session really lacks the permission — a writer on a uid-less row (no
  // handler) has nothing to be told.
  const action =
    canWrite && onWrite ? (
      <Button variant="primary" leadingIcon="edit" onClick={onWrite}>
        Write analysis
      </Button>
    ) : !canWrite ? (
      <p className={styles.permission}>You need permission to edit analyses.</p>
    ) : undefined;
  return (
    <EmptyState
      icon="file-text"
      title="No analysis yet"
      description="Analyses are written by the alert-rca agent, or by anyone allowed to edit analyses."
      {...(action !== undefined ? { action } : {})}
    />
  );
}
