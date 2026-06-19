import { useCallback, useState } from "react";
import { Button } from "@/shared/ui/Button";
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogTitle } from "@/shared/ui/Dialog";
import { toast } from "@/shared/ui/toast/useToast";
import { copyToClipboard } from "@/lib/clipboard";
import type { ContextMenuItem } from "@/shared/ui/DataTableContextMenu";

type WithUid = { uid?: string };

export type ResourceMenuParams<T extends WithUid> = {
  onDelete: (uid: string) => Promise<unknown>;
  extras?: (row: T) => ContextMenuItem[];
};

// eslint-disable-next-line react-refresh/only-export-components
export function buildResourceContextMenu<T extends WithUid>(
  row: T,
  params: ResourceMenuParams<T> & { requestDelete: (row: T) => void },
): ContextMenuItem[] {
  const items: ContextMenuItem[] = [
    {
      key: "copy-json",
      label: "Copy as JSON",
      icon: "copy",
      onSelect: async () => {
        const ok = await copyToClipboard(JSON.stringify(row, null, 2));
        if (ok) toast.success("Copied JSON to clipboard");
        else toast.error("Clipboard unavailable");
      },
    },
    {
      key: "copy-yaml",
      label: "Copy as YAML",
      icon: "copy",
      onSelect: async () => {
        const { stringify } = await import("yaml");
        const ok = await copyToClipboard(stringify(row));
        if (ok) toast.success("Copied YAML to clipboard");
        else toast.error("Clipboard unavailable");
      },
    },
  ];
  if (params.extras) items.push(...params.extras(row));
  items.push({
    key: "delete",
    label: "Delete",
    icon: "trash",
    danger: true,
    disabled: !row.uid,
    onSelect: () => params.requestDelete(row),
  });
  return items;
}

export type ConfirmState<T> = {
  rows: T[];
  title: string;
  message: string;
  busy: boolean;
};

// eslint-disable-next-line react-refresh/only-export-components
export function useConfirmDelete<T extends WithUid>(opts: {
  onDelete: (uid: string) => Promise<unknown>;
  noun: string;
  /** Optional invalidator to refresh related queries after delete. */
  onAfter?: () => void;
}) {
  const [state, setState] = useState<ConfirmState<T> | null>(null);

  const request = useCallback(
    (rows: T[]) => {
      if (rows.length === 0) return;
      const n = rows.length;
      setState({
        rows,
        busy: false,
        title: n === 1 ? `Delete ${opts.noun}?` : `Delete ${n} ${opts.noun}s?`,
        message:
          n === 1
            ? `This will permanently delete the selected ${opts.noun}.`
            : `This will permanently delete ${n} ${opts.noun}s.`,
      });
    },
    [opts.noun],
  );

  const cancel = useCallback(() => setState(null), []);

  const confirm = useCallback(async () => {
    setState((s) => (s ? { ...s, busy: true } : s));
    const rows = state?.rows ?? [];
    const results = await Promise.allSettled(
      rows.map((r) => (r.uid ? opts.onDelete(r.uid) : Promise.reject(new Error("no uid")))),
    );
    const ok = results.filter((r) => r.status === "fulfilled").length;
    const failed = results.length - ok;
    if (failed === 0) {
      toast.success(`Deleted ${ok} ${opts.noun}${ok === 1 ? "" : "s"}`);
    } else if (ok === 0) {
      toast.error(`Failed to delete ${failed} ${opts.noun}${failed === 1 ? "" : "s"}`);
    } else {
      toast.error(`${failed} of ${results.length} deletions failed`);
    }
    opts.onAfter?.();
    setState(null);
  }, [opts, state]);

  return { state, request, cancel, confirm };
}

export function ConfirmDeleteDialog({
  state,
  onCancel,
  onConfirm,
}: {
  state: ConfirmState<unknown> | null;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const open = state !== null;
  return (
    <Dialog open={open} onOpenChange={(o) => (!o ? onCancel() : undefined)}>
      <DialogContent>
        <DialogTitle>{state?.title ?? "Confirm"}</DialogTitle>
        <DialogBody>{state?.message ?? ""}</DialogBody>
        <DialogFooter>
          <Button variant="secondary" onClick={onCancel} disabled={state?.busy}>
            Cancel
          </Button>
          <Button variant="danger" onClick={onConfirm} disabled={state?.busy}>
            {state?.busy ? "Deleting…" : "Delete"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
