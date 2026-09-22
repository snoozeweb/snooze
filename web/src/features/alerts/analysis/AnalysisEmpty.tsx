// The ordinary state of almost every alert: nobody has worked out why it
// fired yet.
//
// It is an empty state rather than an error (the route answers 404 for "no
// analysis"), and it says where analyses come from — an operator who has never
// seen one otherwise has no way to know the tab is ever filled.
import { Button } from "@/shared/ui/Button";
import { EmptyState } from "@/shared/ui/EmptyState";
import { useCanWriteAnalysis } from "./perms";

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
  const action =
    canWrite && onWrite ? (
      <Button variant="primary" leadingIcon="edit" onClick={onWrite}>
        Write analysis
      </Button>
    ) : undefined;
  return (
    <EmptyState
      icon="file-text"
      title="No analysis yet"
      description="The alert-rca agent loop writes analyses onto open alerts; a person with rw_protected can write one here."
      {...(action !== undefined ? { action } : {})}
    />
  );
}
