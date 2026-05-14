import type { ReactNode } from "react";
import * as RT from "@radix-ui/react-tooltip";
import styles from "./Tooltip.module.css";

export function TooltipProvider({
  delay = 200,
  children,
}: {
  delay?: number;
  children: ReactNode;
}) {
  return (
    <RT.Provider delayDuration={delay} skipDelayDuration={300}>
      {children}
    </RT.Provider>
  );
}

export type TooltipProps = {
  content: ReactNode;
  side?: "top" | "right" | "bottom" | "left";
  align?: "start" | "center" | "end";
  children: ReactNode;
};

export function Tooltip({ content, side = "top", align = "center", children }: TooltipProps) {
  if (!content) return <>{children}</>;
  return (
    <RT.Root>
      <RT.Trigger asChild>{children}</RT.Trigger>
      <RT.Portal>
        <RT.Content className={styles.content} side={side} align={align} sideOffset={4}>
          {content}
        </RT.Content>
      </RT.Portal>
    </RT.Root>
  );
}
