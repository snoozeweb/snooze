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
import { CopyField } from "@/shared/ui/CopyField";
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
              {/* CopyField (as CreateApiKeyForm uses) gives copy-success/failure
                  toast feedback — critical for a one-time-shown secret, unlike
                  the old hand-rolled button that failed silently. */}
              <CopyField value={credential.password} aria-label="Password" />
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
