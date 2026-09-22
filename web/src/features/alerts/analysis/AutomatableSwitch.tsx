// "Is this plan safe to run unattended?" — the analysis author's claim.
//
// The rule behind it ("every step is low risk") is written beside the control
// rather than enforced by it: the server stores whatever is sent, and the
// agent loop is the thing that reads the claim. So when the switch is on and
// the plan contains a medium- or high-risk step, the editor says so and lets
// the author decide. An advisory, not a validation failure — turning it into
// one would block a legitimate "I know, run it anyway" and invent a rule the
// server does not have.
import { useId } from "react";
import { useWatch, type Control, type UseFormSetValue } from "react-hook-form";
import { Icon } from "@/shared/icons/Icon";
import { Switch } from "@/shared/ui/Switch";
import type { AnalysisForm, AnalysisStepForm } from "./schema";
import styles from "./editor.module.css";
import own from "./AutomatableSwitch.module.css";

export type AutomatableSwitchProps = {
  control: Control<AnalysisForm>;
  setValue: UseFormSetValue<AnalysisForm>;
};

/** True when any step of either list carries a risk the loop will not accept. */
function hasRiskyStep(steps: readonly AnalysisStepForm[]): boolean {
  return steps.some((s) => s.risk === "medium" || s.risk === "high");
}

export function AutomatableSwitch({ control, setValue }: AutomatableSwitchProps) {
  const baseId = useId();
  const automatable = useWatch({ control, name: "remediation_plan.automatable" }) === true;
  const steps = useWatch({ control, name: "remediation_plan.steps" }) ?? [];
  const rollback = useWatch({ control, name: "remediation_plan.rollback" }) ?? [];
  const risky = hasRiskyStep(steps) || hasRiskyStep(rollback);

  return (
    <div className={styles.field}>
      <div className={own.row}>
        {/* Radix renders a button, which `<label for>` cannot target — the
            visible text names it through aria-labelledby instead. */}
        <Switch
          checked={automatable}
          aria-labelledby={`${baseId}-label`}
          aria-describedby={`${baseId}-hint`}
          onCheckedChange={(v) =>
            setValue("remediation_plan.automatable", v === true, { shouldDirty: true })
          }
        />
        <span className={styles.label} id={`${baseId}-label`}>
          Automatable
        </span>
      </div>
      <p className={styles.hint} id={`${baseId}-hint`}>
        Safe to run unattended only when every step is low risk.
      </p>
      {automatable && risky ? (
        <p className={own.advisory} role="status">
          <Icon name="alert-triangle" size={14} />
          <span>
            Plan has medium or high-risk steps; the agent loop will not run it unattended.
          </span>
        </p>
      ) : null}
    </div>
  );
}
