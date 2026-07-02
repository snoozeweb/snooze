import { useCallback, useState } from "react";
import { Button } from "@/shared/ui/Button";
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogTitle } from "@/shared/ui/Dialog";
import { toast } from "@/shared/ui/toast/useToast";
import { ApiError } from "@/lib/api/client";
import { copyToClipboard } from "@/lib/clipboard";
import type { ContextMenuItem } from "@/shared/ui/DataTableContextMenu";

type WithUid = { uid?: string };

// Best-effort human label for a row in failure messages. Most resources carry a
// name or host; fall back to the uid.
function deleteLabel(row: WithUid): string {
  const r = row as { name?: string; host?: string; uid?: string };
  return r.name ?? r.host ?? r.uid ?? "?";
}

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
  /**
   * Called after the delete settles, with the rows that FAILED (empty on full
   * success). Callers use it to keep only the failed rows selected — so a retry
   * targets exactly them and successfully-deleted rows don't linger selected.
   */
  onAfter?: (failedRows: T[]) => void;
  /**
   * Override the confirm dialog's title/message for high-blast-radius deletes
   * (e.g. a tenant makes all its data inaccessible; a role strips permissions).
   * Pass a STABLE reference (module-level fn or useCallback) so `request` stays
   * identity-stable for DataTable's row memo.
   */
  describe?: ((rows: T[]) => { title?: string; message: string }) | undefined;
}) {
  const [state, setState] = useState<ConfirmState<T> | null>(null);
  // Destructure the stable inputs so `request` depends only on them (callers
  // pass a stable `describe`), keeping its identity stable for DataTable's memo.
  const { noun, describe } = opts;

  const request = useCallback(
    (rows: T[]) => {
      if (rows.length === 0) return;
      const n = rows.length;
      const custom = describe?.(rows);
      setState({
        rows,
        busy: false,
        title: custom?.title ?? (n === 1 ? `Delete ${noun}?` : `Delete ${n} ${noun}s?`),
        message:
          custom?.message ??
          (n === 1
            ? `This will permanently delete the selected ${noun}.`
            : `This will permanently delete ${n} ${noun}s.`),
      });
    },
    [noun, describe],
  );

  const cancel = useCallback(() => setState(null), []);

  const confirm = useCallback(async () => {
    setState((s) => (s ? { ...s, busy: true } : s));
    const rows = state?.rows ?? [];
    const results = await Promise.allSettled(
      rows.map((r) => (r.uid ? opts.onDelete(r.uid) : Promise.reject(new Error("no uid")))),
    );
    const ok = results.filter((r) => r.status === "fulfilled").length;
    const failedRows = rows.filter((_, i) => results[i]?.status === "rejected");
    const failed = failedRows.length;
    if (failed === 0) {
      toast.success(`Deleted ${ok} ${opts.noun}${ok === 1 ? "" : "s"}`);
    } else {
      // Surface the backend's actual reason (e.g. a GuardDelete rejection) and,
      // failing that, which rows failed — never a bare count.
      const firstReason: unknown = results.find(
        (r): r is PromiseRejectedResult => r.status === "rejected",
      )?.reason;
      const detail = firstReason instanceof ApiError ? firstReason.detail : "";
      const because = detail || failedRows.map(deleteLabel).join(", ");
      const prefix =
        ok === 0
          ? `Failed to delete ${failed} ${opts.noun}${failed === 1 ? "" : "s"}`
          : `Deleted ${ok}; ${failed} ${opts.noun}${failed === 1 ? "" : "s"} failed`;
      toast.error(because ? `${prefix}: ${because}` : prefix);
    }
    // Hand back the failed rows so the caller keeps only those selected (retry
    // target). On full success this is empty, which clears the selection.
    opts.onAfter?.(failedRows);
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
