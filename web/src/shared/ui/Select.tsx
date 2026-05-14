import { forwardRef } from "react";
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
  return (
    <RS.Portal>
      <RS.Content className={styles.content} position="popper" sideOffset={4}>
        <RS.Viewport>{children}</RS.Viewport>
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
