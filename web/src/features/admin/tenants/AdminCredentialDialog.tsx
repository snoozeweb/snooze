import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
  DialogBody,
  DialogFooter,
  DialogClose,
} from "@/shared/ui/Dialog";
import { Button } from "@/shared/ui/Button";
import type { AdminCredential } from "./types";

export type AdminCredentialDialogProps = {
  /** null = closed/no credential. */
  credential: AdminCredential | null;
  onClose: () => void;
};

export function AdminCredentialDialog({ credential, onClose }: AdminCredentialDialogProps) {
  if (!credential) return null;
  return (
    <Dialog
      open
      onOpenChange={(o) => {
        if (!o) onClose();
      }}
    >
      <DialogContent>
        <DialogTitle>Admin credential</DialogTitle>
        <DialogBody>
          <DialogDescription>
            A local admin user was provisioned. You won't see this password again — copy it now.
          </DialogDescription>
          <dl>
            <dt>Username</dt>
            <dd>{credential.username}</dd>
            <dt>Password</dt>
            <dd>
              <code>{credential.password}</code>
              <Button
                type="button"
                size="sm"
                variant="ghost"
                onClick={() => void navigator.clipboard?.writeText(credential.password)}
              >
                Copy
              </Button>
            </dd>
          </dl>
        </DialogBody>
        <DialogFooter>
          <DialogClose asChild>
            {/* DialogClose drives open=false → onOpenChange → onClose; no explicit onClick needed. */}
            <Button variant="primary">Done</Button>
          </DialogClose>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
