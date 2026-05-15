import { forwardRef } from "react";
import type { ReactNode } from "react";
import * as RD from "@radix-ui/react-dialog";
import { Icon } from "@/shared/icons/Icon";
import styles from "./Drawer.module.css";

export const Drawer = RD.Root;

export const DrawerTrigger = forwardRef<HTMLButtonElement, RD.DialogTriggerProps>(
  function DrawerTrigger(props, ref) {
    return <RD.Trigger asChild {...props} ref={ref} />;
  },
);

export function DrawerContent({
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

export function DrawerTitle({
  children,
  onClose,
  toolbar,
}: {
  children: ReactNode;
  onClose?: () => void;
  /** Optional content rendered in the header row, just before the close
   *  button. Used by edit forms to place the Enabled switch where the eye
   *  naturally lands — beside the title — instead of buried in the form. */
  toolbar?: ReactNode;
}) {
  return (
    <div className={styles.header}>
      <RD.Title className={styles.title}>{children}</RD.Title>
      {toolbar !== undefined ? <span className={styles.toolbar}>{toolbar}</span> : null}
      <RD.Close asChild>
        <button type="button" className={styles.closeBtn} aria-label="Close" onClick={onClose}>
          <Icon name="x" size={16} />
        </button>
      </RD.Close>
    </div>
  );
}

export function DrawerBody({ children }: { children: ReactNode }) {
  return <div className={styles.body}>{children}</div>;
}

export function DrawerFooter({ children }: { children: ReactNode }) {
  return <div className={styles.footer}>{children}</div>;
}

export const DrawerClose = RD.Close;
