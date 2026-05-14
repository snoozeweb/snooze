import { forwardRef } from "react";
import * as RC from "@radix-ui/react-checkbox";
import { Icon } from "@/shared/icons/Icon";
import styles from "./Checkbox.module.css";

export type CheckboxProps = RC.CheckboxProps;

export const Checkbox = forwardRef<HTMLButtonElement, CheckboxProps>(function Checkbox(
  { className, ...rest },
  ref,
) {
  return (
    <RC.Root ref={ref} className={[styles.root, className].filter(Boolean).join(" ")} {...rest}>
      <RC.Indicator className={styles.indicator}>
        <Icon name="check" size={12} />
      </RC.Indicator>
    </RC.Root>
  );
});
