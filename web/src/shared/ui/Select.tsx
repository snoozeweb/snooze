import { forwardRef, useEffect, useRef } from "react";
import type { ReactNode } from "react";
import * as RS from "@radix-ui/react-select";
import { Icon } from "@/shared/icons/Icon";
import styles from "./Select.module.css";

export const Select = RS.Root;

export type SelectTriggerProps = {
  placeholder?: string;
  className?: string;
};

export const SelectTrigger = forwardRef<HTMLButtonElement, SelectTriggerProps>(
  function SelectTrigger({ placeholder, className }, ref) {
    return (
      <RS.Trigger ref={ref} className={[styles.trigger, className].filter(Boolean).join(" ")}>
        <RS.Value placeholder={placeholder ?? "Select…"} />
        <RS.Icon>
          <Icon name="chevron-down" size={14} />
        </RS.Icon>
      </RS.Trigger>
    );
  },
);

export function SelectContent({ children }: { children: ReactNode }) {
  const contentRef = useRef<HTMLDivElement | null>(null);
  // See MultiCombobox.tsx for the full explanation — Radix Dialog +
  // Radix Select both wrap content in react-remove-scroll, which
  // preventDefaults wheel events targeting non-shard portals. Capture-
  // phase listener at the document level catches wheel events first,
  // scrolls the viewport manually, and stops the event entirely.
  useEffect(() => {
    const popover = contentRef.current;
    if (!popover) return;
    const handler = (e: WheelEvent) => {
      const target = e.target as Node | null;
      if (!target || !popover.contains(target)) return;
      let el: HTMLElement | null = target as HTMLElement;
      while (el && el !== popover) {
        const style = window.getComputedStyle(el);
        const overflowY = style.overflowY;
        if ((overflowY === "auto" || overflowY === "scroll") && el.scrollHeight > el.clientHeight) {
          el.scrollTop += e.deltaY;
          e.preventDefault();
          e.stopImmediatePropagation();
          return;
        }
        el = el.parentElement;
      }
      e.stopImmediatePropagation();
    };
    document.addEventListener("wheel", handler, { capture: true, passive: false });
    return () => document.removeEventListener("wheel", handler, { capture: true });
  }, []);
  return (
    <RS.Portal>
      <RS.Content
        ref={contentRef}
        className={styles.content}
        position="popper"
        sideOffset={4}
        collisionPadding={8}
      >
        <RS.Viewport className={styles.viewport}>{children}</RS.Viewport>
      </RS.Content>
    </RS.Portal>
  );
}

export type SelectItemProps = { value: string; children: ReactNode; disabled?: boolean };

export function SelectItem({ value, children, disabled }: SelectItemProps) {
  return (
    <RS.Item className={styles.item} value={value} {...(disabled ? { disabled: true } : {})}>
      <RS.ItemText>{children}</RS.ItemText>
      <RS.ItemIndicator className={styles.itemIndicator}>
        <Icon name="check" size={12} />
      </RS.ItemIndicator>
    </RS.Item>
  );
}
