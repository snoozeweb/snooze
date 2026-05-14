import { forwardRef } from "react";
import * as RS from "@radix-ui/react-switch";
import styles from "./Switch.module.css";

export type SwitchProps = RS.SwitchProps;

export const Switch = forwardRef<HTMLButtonElement, SwitchProps>(function Switch(
  { className, ...rest },
  ref,
) {
  return (
    <RS.Root ref={ref} className={[styles.root, className].filter(Boolean).join(" ")} {...rest}>
      <RS.Thumb className={styles.thumb} />
    </RS.Root>
  );
});
