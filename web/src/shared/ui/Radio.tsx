import { forwardRef } from "react";
import * as RR from "@radix-ui/react-radio-group";
import styles from "./Radio.module.css";

export type RadioGroupProps = RR.RadioGroupProps;

export const RadioGroup = forwardRef<HTMLDivElement, RadioGroupProps>(function RadioGroup(
  { className, ...rest },
  ref,
) {
  return (
    <RR.Root ref={ref} className={[styles.group, className].filter(Boolean).join(" ")} {...rest} />
  );
});

export type RadioOptionProps = RR.RadioGroupItemProps;

export const RadioOption = forwardRef<HTMLButtonElement, RadioOptionProps>(function RadioOption(
  { className, ...rest },
  ref,
) {
  return (
    <RR.Item ref={ref} className={[styles.option, className].filter(Boolean).join(" ")} {...rest}>
      <RR.Indicator className={styles.indicator} />
    </RR.Item>
  );
});
