import { useSyncExternalStore } from "react";

export type ToastVariant = "success" | "error" | "info";

export type Toast = {
  id: string;
  variant: ToastVariant;
  title?: string;
  description: string;
  traceId?: string;
  duration?: number;
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
  dismiss: (id: string) => toastStore.dismiss(id),
  clear: () => toastStore.clear(),
};

export function useToasts(): readonly Toast[] {
  return useSyncExternalStore(toastStore.subscribe, toastStore.getSnapshot, toastStore.getSnapshot);
}
