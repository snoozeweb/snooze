import { useSyncExternalStore } from "react";

export type ToastVariant = "success" | "error" | "info";

/** A single actionable button rendered inside a toast (e.g. "Undo"). */
export type ToastAction = {
  label: string;
  onSelect: () => void;
};

export type Toast = {
  id: string;
  variant: ToastVariant;
  title?: string;
  description: string;
  traceId?: string;
  duration?: number;
  action?: ToastAction;
};

type Listener = () => void;

class ToastStore {
  private toasts: Toast[] = [];
  private listeners = new Set<Listener>();

  subscribe = (listener: Listener) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  getSnapshot = (): readonly Toast[] => this.toasts;

  push(toast: Omit<Toast, "id">): string {
    const id = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
    this.toasts = [...this.toasts, { id, ...toast }];
    this.emit();
    return id;
  }

  dismiss(id: string) {
    this.toasts = this.toasts.filter((t) => t.id !== id);
    this.emit();
  }

  clear() {
    this.toasts = [];
    this.emit();
  }

  private emit() {
    for (const listener of this.listeners) listener();
  }
}

export const toastStore = new ToastStore();

export const toast = {
  success: (description: string, opts: Partial<Omit<Toast, "variant" | "description">> = {}) =>
    toastStore.push({ variant: "success", description, duration: 3000, ...opts }),
  error: (description: string, opts: Partial<Omit<Toast, "variant" | "description">> = {}) =>
    toastStore.push({ variant: "error", description, duration: 8000, ...opts }),
  info: (description: string, opts: Partial<Omit<Toast, "variant" | "description">> = {}) =>
    toastStore.push({ variant: "info", description, duration: 4000, ...opts }),
  /**
   * undo shows an info toast with an "Undo" action button. It gives the
   * operator an 8s window to reverse a just-completed mutation (the action
   * dismisses the toast and calls `onUndo`). Pass `opts.label` to relabel
   * the button (default "Undo").
   */
  undo: (
    description: string,
    onUndo: () => void,
    opts: Partial<Omit<Toast, "variant" | "description" | "action">> & { label?: string } = {},
  ) => {
    const { label = "Undo", ...rest } = opts;
    let id = "";
    id = toastStore.push({
      variant: "info",
      description,
      duration: 8000,
      ...rest,
      action: {
        label,
        onSelect: () => {
          toastStore.dismiss(id);
          onUndo();
        },
      },
    });
    return id;
  },
  dismiss: (id: string) => toastStore.dismiss(id),
  clear: () => toastStore.clear(),
};

export function useToasts(): readonly Toast[] {
  return useSyncExternalStore(toastStore.subscribe, toastStore.getSnapshot, toastStore.getSnapshot);
}
