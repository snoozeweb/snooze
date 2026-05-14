import { forwardRef } from "react";
import type { ReactNode } from "react";
import * as RD from "@radix-ui/react-dialog";
import styles from "./Dialog.module.css";

export const Dialog = RD.Root;

export const DialogTrigger = forwardRef<HTMLButtonElement, RD.DialogTriggerProps>(
  function DialogTrigger(props, ref) {
    return <RD.Trigger asChild {...props} ref={ref} />;
  },
);

export function DialogContent({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) {
  const classes = [styles.content, className].filter(Boolean).join(" ");
  return (
    <RD.Portal>
      <RD.Overlay className={styles.overlay} />
      <RD.Content className={classes}>{children}</RD.Content>
    </RD.Portal>
  );
}

export function DialogTitle({ children }: { children: ReactNode }) {
  return (
    <div className={styles.header}>
      <RD.Title className={styles.title}>{children}</RD.Title>
    </div>
  );
}

export function DialogDescription({ children }: { children: ReactNode }) {
  return <RD.Description className={styles.description}>{children}</RD.Description>;
}

export function DialogBody({ children }: { children: ReactNode }) {
  return <div className={styles.body}>{children}</div>;
}

export function DialogFooter({ children }: { children: ReactNode }) {
  return <div className={styles.footer}>{children}</div>;
}

export const DialogClose = RD.Close;
