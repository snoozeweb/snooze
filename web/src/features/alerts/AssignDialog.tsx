// AssignDialog — "Assign to…": pick a person from the tenant's directory and
// make them the owner of one alert, the focused one, or a bulk selection.
//
// A sibling of ActionDialog rather than one more entry in its META table: the
// confirm needs a person, not just an optional note, and Assign cannot submit
// without one. The chrome (subjects, note, inline error, Try again) is the
// same so the two read as one family.
import { useEffect, useMemo, useState } from "react";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
} from "@/shared/ui/Dialog";
import { Avatar } from "@/shared/ui/Avatar";
import { Button } from "@/shared/ui/Button";
import { Code } from "@/shared/ui/Code";
import { Combobox, type ComboboxOption } from "@/shared/ui/Combobox";
import { InlineError } from "@/shared/ui/InlineError";
import { Textarea } from "@/shared/ui/Textarea";
import type { ErrorCopy } from "@/lib/api/errorMessage";
import { useAuth } from "@/lib/auth/store";
import { personLabel, usePeople, type Person } from "@/shared/people/api";
import type { Record_ } from "./types";
import styles from "./ActionDialog.module.css";
import own from "./AssignDialog.module.css";

export type AssignInput = { assignee: string; assignee_method: string; message: string };

export type AssignDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  records: Record_[];
  onConfirm: (input: AssignInput) => Promise<void>;
  /** Rows this assign will not touch (closed ones), stated up front. */
  note?: string | undefined;
  submitting?: boolean;
  error?: ErrorCopy | null | undefined;
};

// The option value carries both halves of the identity — the login alone is
// ambiguous in a tenant that has the same login under two auth backends.
const SEP = "\u0000";
function optionValue(p: Person): string {
  return `${p.method}${SEP}${p.name}`;
}
function splitValue(v: string): { method: string; name: string } {
  const i = v.indexOf(SEP);
  return i < 0 ? { method: "", name: v } : { method: v.slice(0, i), name: v.slice(i + 1) };
}

export function AssignDialog({
  open,
  onOpenChange,
  records,
  onConfirm,
  note,
  submitting = false,
  error = null,
}: AssignDialogProps) {
  const { claims } = useAuth();
  const me = claims?.sub ?? "";
  const myMethod = typeof claims?.method === "string" ? claims.method : "";
  const people = usePeople({ enabled: open });
  const [picked, setPicked] = useState<string | undefined>(undefined);
  const [message, setMessage] = useState("");
  const [touched, setTouched] = useState(false);

  useEffect(() => {
    if (open) {
      setPicked(undefined);
      setMessage("");
      setTouched(false);
    }
  }, [open]);

  // The label carries the login too, so typing either finds the person; the
  // signed-in operator is listed first — assigning to yourself is the common
  // case when the alert is not one you would ack yet.
  const { options, byValue } = useMemo(() => {
    const list = [...(people.data ?? [])];
    list.sort((a, b) => Number(b.name === me) - Number(a.name === me));
    const map = new Map<string, Person>();
    const opts: ComboboxOption[] = list.map((p) => {
      const value = optionValue(p);
      map.set(value, p);
      const label = personLabel(p, p.name);
      const suffix = label === p.name ? "" : ` (${p.name})`;
      return { value, label: `${label}${suffix}${p.name === me ? " — you" : ""}` };
    });
    return { options: opts, byValue: map };
  }, [people.data, me]);

  const mine = options.find((o) => {
    const p = byValue.get(o.value);
    return p?.name === me && (!myMethod || p.method === myMethod);
  });
  const missing = touched && picked === undefined;

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setTouched(true);
    if (picked === undefined) return;
    const { method, name } = splitValue(picked);
    void onConfirm({ assignee: name, assignee_method: method, message: message.trim() });
  }

  const n = records.length;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogTitle>{n === 1 ? "Assign alert" : `Assign ${n} alerts`}</DialogTitle>
        <DialogBody>
          <form className={styles.body} onSubmit={handleSubmit} id="assign-form">
            <DialogDescription>
              Make someone the owner. The state does not change — an open alert stays open until its
              owner acknowledges it.
            </DialogDescription>
            {note ? <p className={styles.note}>{note}</p> : null}
            {n <= 8 ? (
              <div className={styles.subjects}>
                {records.map((r) => (
                  <div
                    key={r.uid ?? `${r.host ?? ""}-${r.date_epoch ?? 0}`}
                    className={styles.subjectItem}
                  >
                    <Code>{r.host ?? r.uid ?? "?"}</Code>
                    {r.message ? <span className={styles.subjectMessage}>{r.message}</span> : null}
                  </div>
                ))}
              </div>
            ) : (
              <p className={styles.subjects}>{n} alerts selected.</p>
            )}
            <div>
              <label className={styles.label} htmlFor="assign-assignee">
                Assignee
              </label>
              <div className={own.pickRow}>
                <Combobox
                  id="assign-assignee"
                  className={own.picker!}
                  options={options}
                  value={picked}
                  onValueChange={setPicked}
                  placeholder={people.isPending ? "Loading people…" : "Choose a person"}
                  searchPlaceholder="Search people"
                  noResultsLabel={people.isError ? "Couldn't load the directory" : "Nobody matches"}
                  renderOption={(o) => {
                    const p = byValue.get(o.value);
                    return (
                      <span className={own.option}>
                        <Avatar name={p?.name ?? ""} method={p?.method} size="sm" decorative />
                        <span className={own.optionLabel}>{o.label}</span>
                      </span>
                    );
                  }}
                />
                {mine && picked !== mine.value ? (
                  <Button size="sm" variant="ghost" onClick={() => setPicked(mine.value)}>
                    Assign to me
                  </Button>
                ) : null}
              </div>
              {missing ? (
                <p className={own.fieldError} role="alert">
                  Choose who should own {n === 1 ? "this alert" : "these alerts"}.
                </p>
              ) : null}
            </div>
            <label htmlFor="assign-message">
              <span className={styles.label}>Message (optional)</span>
              <Textarea
                id="assign-message"
                placeholder="Optional context for the new owner"
                value={message}
                onChange={(e) => setMessage(e.target.value)}
                rows={2}
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
            form="assign-form"
            variant="primary"
            loading={submitting}
            disabled={submitting}
          >
            {error ? "Try again" : "Assign"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
