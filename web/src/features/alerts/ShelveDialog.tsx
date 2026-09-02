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
import { InlineError } from "@/shared/ui/InlineError";
import type { ErrorCopy } from "@/lib/api/errorMessage";
import type { Record_ } from "./types";
import { SHELVE_VS_SNOOZE_HINT } from "./silencingGuide";
import styles from "./ShelveDialog.module.css";

export type ShelveDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  records: Record_[];
  onConfirm: (input: { duration: number; message: string }) => Promise<void>;
  submitting?: boolean;
  /** The most recent failed attempt, rendered inline above the footer. The
   *  dialog stays open and the confirm button re-enabled (see `submitting`)
   *  so retrying is just clicking Shelve/Try again — mirrors ActionDialog's
   *  Phase 3 pattern (a failed shelve used to close the dialog on failure,
   *  same anti-pattern that fix addressed elsewhere). Cleared by the caller
   *  when a new attempt starts. */
  error?: ErrorCopy | null | undefined;
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
  error = null,
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
        {/* Shelve and Snooze are the two verbs operators mix up: one parks a
            single row, the other stops the whole class of alert from paging.
            Say which is which here, where the wrong choice is one click away.
            See silencingGuide.ts for the server behaviour behind the claim. */}
        <p className={styles.hint}>{SHELVE_VS_SNOOZE_HINT}</p>
        <DialogBody>
          <form className={styles.body} onSubmit={handleSubmit} id="shelve-form">
            {/* "Silenced" was a lie: shelving does not stop notifications —
                notification.Process skips only ack and close
                (internal/pluginimpl/notification/plugin.go:293-296), so a
                shelved alert that recurs still pages. What it actually does is
                park the row under the Shelved tab until the chosen window
                expires. The Snooze cross-link is one line above. */}
            <DialogDescription>
              {records.length === 1 ? "The alert moves" : "The alerts move"} to the Shelved tab
              until the duration expires, then {records.length === 1 ? "returns" : "return"} to Open
              on {records.length === 1 ? "its" : "their"} own.
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
            {error ? <InlineError {...error} /> : null}
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
            {error ? "Try again" : "Shelve"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
