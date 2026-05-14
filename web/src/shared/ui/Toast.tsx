import type { ReactNode } from "react";
import * as RT from "@radix-ui/react-toast";
import { Icon } from "@/shared/icons/Icon";
import { toast as toastApi, useToasts } from "./toast/useToast";
import type { ToastVariant } from "./toast/useToast";
import styles from "./Toast.module.css";

export function ToastProvider({ children }: { children: ReactNode }) {
  return <RT.Provider swipeDirection="right">{children}</RT.Provider>;
}

export function Toaster() {
  const toasts = useToasts();
  return (
    <>
      {toasts.map((t) => (
        <RT.Root
          key={t.id}
          {...(t.duration !== undefined ? { duration: t.duration } : {})}
          onOpenChange={(open) => {
            if (!open) toastApi.dismiss(t.id);
          }}
          className={`${styles.toast} ${variantClass(t.variant)}`}
        >
          <div className={styles.body}>
            {t.title ? <RT.Title className={styles.title}>{t.title}</RT.Title> : null}
            <RT.Description className={styles.description}>{t.description}</RT.Description>
            {t.traceId ? <span className={styles.trace}>trace {t.traceId}</span> : null}
          </div>
          <RT.Close className={styles.closeBtn} aria-label="Dismiss">
            <Icon name="x" size={14} />
          </RT.Close>
        </RT.Root>
      ))}
      <RT.Viewport className={styles.viewport} />
    </>
  );
}

function variantClass(v: ToastVariant) {
  return v === "success" ? styles.success : v === "error" ? styles.error : styles.info;
}
