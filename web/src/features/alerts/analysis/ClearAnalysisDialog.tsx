// Confirm removing an alert's analysis.
//
// A dialog rather than an undo toast: the write is a whole-subtree DELETE with
// nothing kept server-side, so there is no undo to offer — the honest thing is
// to ask first and say plainly that the loop may write a new one later, which
// is the question an operator actually has ("am I destroying the only copy, or
// will it come back?").
import { useState } from "react";
import { Button } from "@/shared/ui/Button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
} from "@/shared/ui/Dialog";
import { InlineError } from "@/shared/ui/InlineError";
import { useAnnounce } from "@/shared/a11y/LiveAnnouncer";
import { toast } from "@/shared/ui/toast/useToast";
import { describeError, type ErrorCopy } from "@/lib/api/errorMessage";
import { useClearAnalysis } from "./api";

export type ClearAnalysisDialogProps = {
  /** The alert whose analysis is being removed. */
  uid: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Called after the DELETE succeeds, once the dialog has closed. */
  onCleared?: (() => void) | undefined;
};

export function ClearAnalysisDialog({
  uid,
  open,
  onOpenChange,
  onCleared,
}: ClearAnalysisDialogProps) {
  const clear = useClearAnalysis();
  const announce = useAnnounce();
  const [failure, setFailure] = useState<ErrorCopy | null>(null);

  async function confirm() {
    setFailure(null);
    try {
      await clear.mutateAsync(uid);
      announce("Analysis removed");
      toast.success("Analysis removed");
      onOpenChange(false);
      onCleared?.();
    } catch (err) {
      setFailure(describeError(err, "Could not remove the analysis."));
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) setFailure(null);
        onOpenChange(next);
      }}
    >
      <DialogContent>
        <DialogTitle>Remove this analysis?</DialogTitle>
        <DialogBody>
          <DialogDescription>
            This cannot be undone. The alert-rca agent loop may analyse the alert again later.
          </DialogDescription>
          {failure !== null ? <InlineError {...failure} /> : null}
        </DialogBody>
        <DialogFooter>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            Keep analysis
          </Button>
          <Button variant="danger" loading={clear.isPending} onClick={() => void confirm()}>
            Remove analysis
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
