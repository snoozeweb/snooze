// One step of a remediation (or rollback) plan.
//
// Three facts, in the order an operator needs them: what to do, how dangerous
// it is, and the exact command. Risk is marked only when it is worth a second
// look — medium or high. A "Low risk" chip on most steps of most plans was
// noise that taught the eye to skip the chip, including on the step where it
// mattered (the dashboard's Analyses rows already followed this rule). The command is the part that gets copied and
// pasted into a terminal, so it is set in a code block with its own copy
// button and scrolls horizontally rather than wrapping — a wrapped shell line
// is a line you cannot trust after pasting.
import { useCallback } from "react";
import { CodeBlock } from "@/shared/ui/Code";
import { CollapsibleSection } from "@/shared/ui/CollapsibleSection";
import { IconButton } from "@/shared/ui/IconButton";
import { toast } from "@/shared/ui/toast/useToast";
import type { components } from "@/lib/api/types.gen";
import { isRisk } from "./enums";
import { RiskBadge } from "./RiskBadge";
import styles from "./StepCard.module.css";

export type AgenticStep = components["schemas"]["AgenticStep"];

export type StepCardProps = {
  /** 1-based position, printed beside the action. */
  index: number;
  step: AgenticStep;
  /**
   * Fold the command behind a disclosure, leaving action + risk on one line.
   * PlanView turns this on for long plans (> 5 steps), where a column of open
   * code blocks buries the shape of the plan.
   */
  collapsible?: boolean;
};

function CommandBlock({ command }: { command: string }) {
  const copy = useCallback(() => {
    void (async () => {
      try {
        await navigator.clipboard.writeText(command);
        toast.success("Command copied");
      } catch {
        toast.error("Copy failed — select and copy manually");
      }
    })();
  }, [command]);
  // The copy button hangs off the WRAPPER, not the <pre>: the pre is the
  // horizontal scroll container, and a button positioned inside it would slide
  // out of view the moment a long command was scrolled.
  return (
    <div className={styles.commandWrap}>
      <CodeBlock className={styles.command!}>{command}</CodeBlock>
      <IconButton
        className={styles.copy}
        icon="copy"
        label="Copy command"
        size="sm"
        onClick={copy}
      />
    </div>
  );
}

export function StepCard({ index, step, collapsible = false }: StepCardProps) {
  const command = step.command?.trim() ?? "";
  const risk = isRisk(step.risk) && step.risk !== "low" ? step.risk : undefined;
  return (
    <li className={styles.step}>
      <div className={styles.head}>
        <span className={styles.index}>{index}</span>
        <p className={styles.action}>{step.action}</p>
        {risk ? <RiskBadge risk={risk} className={styles.risk!} /> : null}
      </div>
      {command === "" ? null : collapsible ? (
        <CollapsibleSection title="Command" summary={command.split("\n")[0]}>
          <CommandBlock command={command} />
        </CollapsibleSection>
      ) : (
        <CommandBlock command={command} />
      )}
    </li>
  );
}
