import type React from "react";
import * as RC from "@radix-ui/react-checkbox";
import { Icon } from "@/shared/icons/Icon";
import styles from "./Checkbox.module.css";

export type CheckboxProps = RC.CheckboxProps & { ref?: React.Ref<HTMLButtonElement> };

export function Checkbox({ className, ref, ...rest }: CheckboxProps) {
  return (
    <RC.Root ref={ref} className={[styles.root, className].filter(Boolean).join(" ")} {...rest}>
      <RC.Indicator className={styles.indicator}>
        <Icon name="check" size={12} />
      </RC.Indicator>
    </RC.Root>
  );
}
