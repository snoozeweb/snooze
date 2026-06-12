import type React from "react";
import * as RR from "@radix-ui/react-radio-group";
import styles from "./Radio.module.css";

export type RadioGroupProps = RR.RadioGroupProps & { ref?: React.Ref<HTMLDivElement> };

export function RadioGroup({ className, ref, ...rest }: RadioGroupProps) {
  return (
    <RR.Root ref={ref} className={[styles.group, className].filter(Boolean).join(" ")} {...rest} />
  );
}

export type RadioOptionProps = RR.RadioGroupItemProps & { ref?: React.Ref<HTMLButtonElement> };

export function RadioOption({ className, ref, ...rest }: RadioOptionProps) {
  return (
    <RR.Item ref={ref} className={[styles.option, className].filter(Boolean).join(" ")} {...rest}>
      <RR.Indicator className={styles.indicator} />
    </RR.Item>
  );
}
