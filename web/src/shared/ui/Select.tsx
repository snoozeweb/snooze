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
  // Stop wheel events from bubbling to document, where Radix Dialog's
  // react-remove-scroll listener would preventDefault() and break mousewheel
  // scrolling on Select dropdowns opened from inside a Drawer.
  useEffect(() => {
    const el = contentRef.current;
    if (!el) return;
    const handler = (e: WheelEvent) => e.stopPropagation();
    el.addEventListener("wheel", handler);
    return () => el.removeEventListener("wheel", handler);
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
