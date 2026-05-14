import { forwardRef } from "react";
import type { ReactNode } from "react";
import * as RP from "@radix-ui/react-popover";
import styles from "./Popover.module.css";

export const Popover = RP.Root;

export const PopoverTrigger = forwardRef<HTMLButtonElement, RP.PopoverTriggerProps>(
  function PopoverTrigger(props, ref) {
    return <RP.Trigger {...props} ref={ref} />;
  },
);

export type PopoverContentProps = {
  side?: "top" | "right" | "bottom" | "left";
  align?: "start" | "center" | "end";
  className?: string;
  children: ReactNode;
};

export function PopoverContent({
  side = "bottom",
  align = "start",
  className,
  children,
}: PopoverContentProps) {
  const classes = [styles.content, className].filter(Boolean).join(" ");
  return (
    <RP.Portal>
      <RP.Content className={classes} side={side} align={align} sideOffset={4} collisionPadding={8}>
        {children}
      </RP.Content>
    </RP.Portal>
  );
}
