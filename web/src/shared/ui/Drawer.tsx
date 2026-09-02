import type { ReactNode } from "react";
import type React from "react";
import * as RD from "@radix-ui/react-dialog";
import { Icon } from "@/shared/icons/Icon";
import styles from "./Drawer.module.css";

export const Drawer = RD.Root;

export type DrawerTriggerProps = RD.DialogTriggerProps & { ref?: React.Ref<HTMLButtonElement> };

export function DrawerTrigger({ ref, ...props }: DrawerTriggerProps) {
  return <RD.Trigger asChild {...props} ref={ref} />;
}

export function DrawerContent({
  children,
  className,
  wide = false,
  onOpenAutoFocus,
}: {
  children: ReactNode;
  className?: string;
  /** Opt-in wide variant for read surfaces (e.g. the DataTable detail
   *  drawer). Edit drawers keep the default 560px width. */
  wide?: boolean;
  /** Radix pass-through: override where focus lands when the drawer opens.
   *  Radix's default focuses the first tabbable element, which pops that
   *  element's Tooltip when it's an IconButton — read drawers redirect the
   *  initial focus to a neutral container instead. */
  onOpenAutoFocus?: (e: Event) => void;
}) {
  const classes = [styles.content, wide ? styles.wide : "", className].filter(Boolean).join(" ");
  return (
    <RD.Portal>
      <RD.Overlay className={styles.overlay} />
      <RD.Content className={classes} {...(onOpenAutoFocus ? { onOpenAutoFocus } : {})}>
        {children}
      </RD.Content>
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
        {/* "Close panel", not "Close": on the alerts drawer this ✕ sits beside
            a "Close alert" button, and two controls both named "Close" is the
            kind of ambiguity that gets an alert closed by accident. */}
        <button
          type="button"
          className={styles.closeBtn}
          aria-label="Close panel"
          onClick={onClose}
        >
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
