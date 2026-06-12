import type React from "react";
import * as RS from "@radix-ui/react-switch";
import styles from "./Switch.module.css";

export type SwitchProps = RS.SwitchProps & { ref?: React.Ref<HTMLButtonElement> };

export function Switch({ className, ref, ...rest }: SwitchProps) {
  return (
    <RS.Root ref={ref} className={[styles.root, className].filter(Boolean).join(" ")} {...rest}>
      <RS.Thumb className={styles.thumb} />
    </RS.Root>
  );
}
