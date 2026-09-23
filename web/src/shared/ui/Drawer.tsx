import { useEffect } from "react";
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

/**
 * Reserves a side panel's width on the app shell while it is on screen.
 *
 * A side panel is not a modal: the page behind it stays live, so it must not
 * sit ON that page. The panel publishes its own width and the shell yields it
 * (AppShell.module.css `html[data-side-panel="open"]`), which is what makes the
 * pager, the lifecycle tabs and the sort headers clickable at all while a panel
 * is open — as a modal with a full-screen scrim, every one of them was inert.
 *
 * A stack, not a boolean: an editor opened from inside another panel must
 * restore the previous reservation when it closes, not clear it.
 */
const panelWidths: string[] = [];
function useSidePanelWidth(active: boolean, width: string): void {
  useEffect(() => {
    if (!active) return;
    const root = document.documentElement;
    const apply = () => {
      const top = panelWidths[panelWidths.length - 1];
      if (top === undefined) {
        delete root.dataset["sidePanel"];
        root.style.removeProperty("--open-panel-width");
        return;
      }
      root.dataset["sidePanel"] = "open";
      root.style.setProperty("--open-panel-width", top);
    };
    panelWidths.push(width);
    apply();
    return () => {
      const at = panelWidths.lastIndexOf(width);
      if (at >= 0) panelWidths.splice(at, 1);
      apply();
    };
  }, [active, width]);
}

export function DrawerContent({
  children,
  className,
  wide = false,
  nonModal = false,
  onOpenAutoFocus,
  onPointerDownOutside,
  onInteractOutside,
}: {
  children: ReactNode;
  className?: string;
  /** Opt-in wide variant for read surfaces (e.g. the DataTable detail
   *  drawer). Edit drawers keep the default 560px width. */
  wide?: boolean;
  /**
   * Render as a side panel rather than a modal: no scrim over the app.
   *
   * Pair it with `modal={false}` on the `Drawer` root. A read drawer sits
   * BESIDE the table it was opened from, and the operator keeps working in
   * that table — paging, sorting, opening another row. The scrim is what made
   * that impossible: a fixed full-screen layer at `--z-modal` swallowed every
   * click behind it, so "next page" with the inspector open did nothing at
   * all. Edit drawers hold unsaved work and keep the modal treatment.
   */
  nonModal?: boolean;
  /** Radix pass-through: override where focus lands when the drawer opens.
   *  Radix's default focuses the first tabbable element, which pops that
   *  element's Tooltip when it's an IconButton — read drawers redirect the
   *  initial focus to a neutral container instead. */
  onOpenAutoFocus?: (e: Event) => void;
  /** Radix pass-through: veto the outside-click dismissal (a side panel that
   *  closed whenever the operator touched the table behind it would undo the
   *  point of being non-modal). */
  onPointerDownOutside?: (e: Event) => void;
  onInteractOutside?: (e: Event) => void;
}) {
  // Radix mounts Content only while the drawer is open, so being rendered at
  // all IS the "a panel is on screen" signal.
  useSidePanelWidth(nonModal, wide ? "var(--panel-width)" : "var(--panel-width-edit)");
  const classes = [styles.content, wide ? styles.wide : "", nonModal ? styles.panel : "", className]
    .filter(Boolean)
    .join(" ");
  return (
    <RD.Portal>
      {nonModal ? null : <RD.Overlay className={styles.overlay} />}
      <RD.Content
        className={classes}
        {...(onOpenAutoFocus ? { onOpenAutoFocus } : {})}
        {...(onPointerDownOutside ? { onPointerDownOutside } : {})}
        {...(onInteractOutside ? { onInteractOutside } : {})}
      >
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
