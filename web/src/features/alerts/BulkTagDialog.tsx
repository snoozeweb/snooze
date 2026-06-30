import { useEffect, useId, useRef, useState } from "react";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
} from "@/shared/ui/Dialog";
import { Button } from "@/shared/ui/Button";
import { toast } from "@/shared/ui/toast/useToast";
import { ApiError } from "@/lib/api/client";
import { useBulkUpdateRecord } from "./api";
import styles from "./BulkTagDialog.module.css";

// ── KV pair form ─────────────────────────────────────────────────────────────

type KvPair = { id: string; key: string; value: string };

function KvSection({ pairs, onChange }: { pairs: KvPair[]; onChange: (pairs: KvPair[]) => void }) {
  const addPair = () =>
    onChange([...pairs, { id: Math.random().toString(36).slice(2), key: "", value: "" }]);

  const removePair = (id: string) => onChange(pairs.filter((p) => p.id !== id));

  const updatePair = (id: string, field: "key" | "value", val: string) =>
    onChange(pairs.map((p) => (p.id === id ? { ...p, [field]: val } : p)));

  return (
    <div role="group" className={styles.section} aria-label="Set attributes">
      <span className={styles.sectionLabel}>Set attributes</span>
      {pairs.map((p) => (
        <div key={p.id} className={styles.kvRow}>
          <input
            aria-label="Attribute key"
            className={styles.kvInput}
            placeholder="key"
            value={p.key}
            onChange={(e) => updatePair(p.id, "key", e.target.value)}
          />
          <input
            aria-label="Attribute value"
            className={styles.kvInput}
            placeholder="value"
            value={p.value}
            onChange={(e) => updatePair(p.id, "value", e.target.value)}
          />
          <button
            type="button"
            aria-label="Remove pair"
            className={styles.removeBtn}
            onClick={() => removePair(p.id)}
          >
            ×
          </button>
        </div>
      ))}
      <button type="button" className={styles.addLink} onClick={addPair}>
        + Add pair
      </button>
    </div>
  );
}

// ── Chip tag input ────────────────────────────────────────────────────────────

function ChipInput({
  label,
  chips,
  onChange,
}: {
  label: string;
  chips: string[];
  onChange: (chips: string[]) => void;
}) {
  const [draft, setDraft] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);
  const id = useId();

  const commit = (raw: string) => {
    const tags = raw
      .split(/[\s,]+/)
      .map((t) => t.trim())
      .filter(Boolean);
    if (tags.length === 0) return;
    const next = [...new Set([...chips, ...tags])];
    onChange(next);
    setDraft("");
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter" || e.key === "," || e.key === " ") {
      e.preventDefault();
      commit(draft);
    } else if (e.key === "Backspace" && draft === "" && chips.length > 0) {
      onChange(chips.slice(0, -1));
    }
  };

  const handleBlur = () => {
    if (draft.trim()) commit(draft);
  };

  const removeChip = (tag: string) => onChange(chips.filter((c) => c !== tag));

  return (
    <div role="group" className={styles.section} aria-label={label}>
      <label htmlFor={id} className={styles.sectionLabel}>
        {label}
      </label>
      <div className={styles.chipInput}>
        {chips.map((chip) => (
          <span key={chip} className={styles.chip}>
            {chip}
            <button
              type="button"
              aria-label={`Remove tag ${chip}`}
              className={styles.chipRemove}
              onClick={(e) => {
                e.stopPropagation();
                removeChip(chip);
              }}
            >
              ×
            </button>
          </span>
        ))}
        <input
          id={id}
          ref={inputRef}
          className={styles.chipTextInput}
          placeholder={chips.length === 0 ? "Type a tag and press Space or Enter" : ""}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={handleKeyDown}
          onBlur={handleBlur}
        />
      </div>
    </div>
  );
}

// ── BulkTagDialog ─────────────────────────────────────────────────────────────

export type BulkTagDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  q: string | undefined;
  recordCount: number;
};

export function BulkTagDialog({ open, onOpenChange, q, recordCount }: BulkTagDialogProps) {
  const bulkUpdateMut = useBulkUpdateRecord();
  const [pairs, setPairs] = useState<KvPair[]>([{ id: "init", key: "", value: "" }]);
  const [tagChips, setTagChips] = useState<string[]>([]);
  const [untagChips, setUntagChips] = useState<string[]>([]);

  useEffect(() => {
    if (open) {
      setPairs([{ id: "init", key: "", value: "" }]);
      setTagChips([]);
      setUntagChips([]);
    }
  }, [open]);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();

    const set: Record<string, unknown> = {};
    for (const p of pairs) {
      if (p.key.trim()) set[p.key.trim()] = p.value;
    }
    const hasSet = Object.keys(set).length > 0;
    const hasTag = tagChips.length > 0;
    const hasUntag = untagChips.length > 0;

    if (!hasSet && !hasTag && !hasUntag) {
      toast.error("Fill in at least one field (set, tag, or untag) before submitting.");
      return;
    }

    void (async () => {
      try {
        const resp = await bulkUpdateMut.mutateAsync({
          ...(q ? { q } : {}),
          ...(hasSet ? { set } : {}),
          ...(hasTag ? { tag: tagChips } : {}),
          ...(hasUntag ? { untag: untagChips } : {}),
        });
        const parts: string[] = [];
        if (resp.set > 0) parts.push(`${resp.set} set`);
        if (resp.tagged > 0) parts.push(`${resp.tagged} tagged`);
        if (resp.untagged > 0) parts.push(`${resp.untagged} untagged`);
        toast.success(`${resp.matched} matched — ${parts.join(", ")}`);
        onOpenChange(false);
      } catch (e) {
        const detail = e instanceof ApiError ? e.detail : "Bulk update failed";
        toast.error(detail);
      }
    })();
  };

  const title = recordCount > 0 ? `Tag / set fields (${recordCount})` : "Tag / set fields";

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogTitle>{title}</DialogTitle>
        <DialogBody>
          <form className={styles.body} onSubmit={handleSubmit} id="bulk-tag-form">
            <DialogDescription>
              Apply attribute changes or add/remove tags across the selected alerts in a single
              operation.
            </DialogDescription>
            <KvSection pairs={pairs} onChange={setPairs} />
            <ChipInput label="Add tags" chips={tagChips} onChange={setTagChips} />
            <ChipInput label="Remove tags" chips={untagChips} onChange={setUntagChips} />
          </form>
        </DialogBody>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            type="submit"
            form="bulk-tag-form"
            variant="primary"
            loading={bulkUpdateMut.isPending}
            disabled={bulkUpdateMut.isPending}
          >
            Apply
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
