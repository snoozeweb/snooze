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
import styles from "./ShelveDialog.module.css";

export type ShelveDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  records: Record_[];
  onConfirm: (input: { duration: number; message: string }) => Promise<void>;
  submitting?: boolean;
};

const DURATION_OPTIONS = [
  { label: "1h", value: 3600 },
  { label: "4h", value: 14400 },
  { label: "8h", value: 28800 },
  { label: "24h", value: 86400 },
  { label: "48h", value: 172800 },
  { label: "Custom", value: "custom" as const },
];

export function ShelveDialog({
  open,
  onOpenChange,
  records,
  onConfirm,
  submitting = false,
}: ShelveDialogProps) {
  const [durationValue, setDurationValue] = useState<number | "custom">(14400);
  const [customHours, setCustomHours] = useState("4");
  const [message, setMessage] = useState("");

  useEffect(() => {
    if (open) {
      setDurationValue(14400);
      setCustomHours("4");
      setMessage("");
    }
  }, [open]);

  const showCustom = durationValue === "custom";

  const effectiveDuration: number = showCustom
    ? Math.max(1, parseInt(customHours, 10) || 1) * 3600
    : durationValue;

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    void onConfirm({ duration: effectiveDuration, message: message.trim() });
  }

  const title = records.length === 1 ? "Shelve alert" : `Shelve ${records.length} alerts`;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogTitle>{title}</DialogTitle>
        <DialogBody>
          <form className={styles.body} onSubmit={handleSubmit} id="shelve-form">
            <DialogDescription>
              The alert will be silenced until the duration expires, then automatically returned to
              open.
            </DialogDescription>
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
              <span className={styles.label}>Duration</span>
              <select
                aria-label="Duration"
                value={String(durationValue)}
                onChange={(e) => {
                  const v = e.target.value;
                  setDurationValue(v === "custom" ? "custom" : parseInt(v, 10));
                }}
              >
                {DURATION_OPTIONS.map((opt) => (
                  <option key={String(opt.value)} value={String(opt.value)}>
                    {opt.label}
                  </option>
                ))}
              </select>
            </label>
            {showCustom ? (
              <label>
                <span className={styles.label}>Hours</span>
                <input
                  type="number"
                  aria-label="Hours"
                  min="1"
                  step="1"
                  value={customHours}
                  onChange={(e) => setCustomHours(e.target.value)}
                />
              </label>
            ) : null}
            <label htmlFor="shelve-message">
              <span className={styles.label}>Message (optional)</span>
              <Textarea
                id="shelve-message"
                placeholder="Optional context"
                value={message}
                onChange={(e) => setMessage(e.target.value)}
                rows={3}
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
            form="shelve-form"
            variant="primary"
            loading={submitting}
            disabled={submitting}
          >
            Shelve
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
