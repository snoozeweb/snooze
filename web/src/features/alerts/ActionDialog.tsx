import { useEffect, useState } from "react";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
} from "@/shared/ui/Dialog";
import { Button } from "@/shared/ui/Button";
import { Textarea } from "@/shared/ui/Textarea";
import { Code } from "@/shared/ui/Code";
import type { Record_ } from "./types";
import styles from "./ActionDialog.module.css";

export type ActionType = "ack" | "close" | "open" | "esc" | "comment";

const META: Record<
  ActionType,
  {
    title: (n: number) => string;
    confirmLabel: string;
    description: string;
    variant: "primary" | "danger";
    requireMessage: boolean;
  }
> = {
  ack: {
    title: (n) => (n === 1 ? "Acknowledge alert" : `Acknowledge ${n} alerts`),
    confirmLabel: "Acknowledge",
    description: "Add an optional note that will be saved with each alert.",
    variant: "primary",
    requireMessage: false,
  },
  close: {
    title: (n) => (n === 1 ? "Close alert" : `Close ${n} alerts`),
    confirmLabel: "Close",
    description: "Mark the alert(s) as resolved. They will move to the closed view.",
    variant: "primary",
    requireMessage: false,
  },
  open: {
    title: (n) => (n === 1 ? "Re-open alert" : `Re-open ${n} alerts`),
    confirmLabel: "Re-open",
    description: "Reopen the alert(s). They will return to the open view.",
    variant: "primary",
    requireMessage: false,
  },
  esc: {
    title: (n) => (n === 1 ? "Re-escalate alert" : `Re-escalate ${n} alerts`),
    confirmLabel: "Re-escalate",
    description:
      "Reset the alert(s) and fire notifications again. A message is recommended so operators see why.",
    variant: "primary",
    requireMessage: false,
  },
  comment: {
    title: (n) => (n === 1 ? "Comment on alert" : `Comment on ${n} alerts`),
    confirmLabel: "Comment",
    description: "Add a note to the alert(s) without changing their state.",
    variant: "primary",
    requireMessage: true,
  },
};

export type ActionDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  actionType: ActionType;
  records: Record_[];
  onConfirm: (input: { message: string }) => Promise<void>;
  submitting?: boolean;
};

export function ActionDialog({
  open,
  onOpenChange,
  actionType,
  records,
  onConfirm,
  submitting = false,
}: ActionDialogProps) {
  const meta = META[actionType];
  const [message, setMessage] = useState("");
  const [touched, setTouched] = useState(false);

  useEffect(() => {
    if (open) {
      setMessage("");
      setTouched(false);
    }
  }, [open, actionType]);

  const messageInvalid = meta.requireMessage && message.trim().length === 0;

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setTouched(true);
    if (messageInvalid) return;
    void onConfirm({ message: message.trim() });
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogTitle>{meta.title(records.length)}</DialogTitle>
        <DialogBody>
          <form className={styles.body} onSubmit={handleSubmit} id="action-form">
            <DialogDescription>{meta.description}</DialogDescription>
            {records.length <= 8 ? (
              <div className={styles.subjects}>
                {records.map((r) => (
                  <div
                    key={r.uid ?? `${r.host ?? ""}-${r.date_epoch ?? 0}`}
                    className={styles.subjectItem}
                  >
                    <Code>{r.host ?? r.uid ?? "?"}</Code>
                  </div>
                ))}
              </div>
            ) : (
              <p className={styles.subjects}>{records.length} alerts selected.</p>
            )}
            <label>
              <span className={styles.label}>
                Message{meta.requireMessage ? "" : " (optional)"}
              </span>
              <Textarea
                placeholder={actionType === "comment" ? "Type your comment" : "Optional context"}
                value={message}
                onChange={(e) => setMessage(e.target.value)}
                rows={3}
                {...(touched && messageInvalid ? { invalid: true } : {})}
              />
            </label>
          </form>
        </DialogBody>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            type="submit"
            form="action-form"
            variant={meta.variant}
            loading={submitting}
            disabled={submitting}
          >
            {meta.confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
