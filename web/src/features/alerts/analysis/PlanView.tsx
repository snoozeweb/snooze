// What to do about the alert: the ordered steps, the undo, and whether the
// plan is safe to run unattended.
//
// "Automatable" is read-only here and stays read-only: it is the analysis
// author's claim, and the rule behind it ("every step is low risk") is spelled
// out beside it so nobody has to infer it from a tick.
import type { components } from "@/lib/api/types.gen";
import { StepCard } from "./StepCard";
import styles from "./PlanView.module.css";

export type AgenticRemediationPlan = components["schemas"]["AgenticRemediationPlan"];

export type PlanViewProps = {
  plan: AgenticRemediationPlan | undefined;
};

/** Beyond this, a column of open code blocks buries the shape of the plan. */
const COLLAPSE_ABOVE = 5;

export function PlanView({ plan }: PlanViewProps) {
  const steps = plan?.steps ?? [];
  const rollback = plan?.rollback ?? [];
  const automatable = plan?.automatable === true;
  if (steps.length === 0 && rollback.length === 0) return null;

  return (
    <section className={styles.plan}>
      <h3 className={styles.heading}>Remediation plan</h3>
      {steps.length > 0 ? (
        <ol className={styles.steps}>
          {steps.map((step, i) => (
            <StepCard
              key={`${i}-${step.action}`}
              index={i + 1}
              step={step}
              collapsible={steps.length > COLLAPSE_ABOVE}
            />
          ))}
        </ol>
      ) : null}

      {rollback.length > 0 ? (
        <>
          <h4 className={styles.subheading}>Rollback</h4>
          <ol className={styles.steps}>
            {rollback.map((step, i) => (
              <StepCard
                key={`${i}-${step.action}`}
                index={i + 1}
                step={step}
                collapsible={rollback.length > COLLAPSE_ABOVE}
              />
            ))}
          </ol>
        </>
      ) : null}

      <p className={styles.automatable}>
        <span className={styles.automatableValue}>
          {"Automatable: "}
          <strong>{automatable ? "Yes" : "No"}</strong>
        </span>
        <span className={styles.automatableHint}>
          Safe to run unattended only when every step is low risk.
        </span>
      </p>
    </section>
  );
}
