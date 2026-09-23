// What to do about the alert: the ordered steps, grouped by when to run them,
// and the undo.
//
// Whether the plan may run unattended is stated ONCE, at the top, beside the
// section's heading — "Automatable" or "Manual" — instead of as an
// "Automatable: No" footer the reader met only after every step. It is read
// before the steps because it changes how they are read: an automatable plan
// is a list to approve, a manual one a list to carry out. The flag stays
// read-only here; it is the analysis author's claim.
import { useId } from "react";
import type { components } from "@/lib/api/types.gen";
import { AutomatableBadge } from "./AutomatableBadge";
import { Disclosure } from "./Disclosure";
import { StepCard, type AgenticStep } from "./StepCard";
import { groupSteps, type IndexedStep } from "./verdict";
import styles from "./PlanView.module.css";

export type AgenticRemediationPlan = components["schemas"]["AgenticRemediationPlan"];

export type PlanViewProps = {
  plan: AgenticRemediationPlan | undefined;
};

/** Beyond this, a column of open code blocks buries the shape of the plan. */
const COLLAPSE_ABOVE = 5;

/** What "Manual" means, for the one reader who wonders why it is not automatic. */
const MANUAL_HINT = "Needs a human: at least one step is risky or needs judgement";

function StepList({
  steps,
  collapsible,
  labelledBy,
}: {
  steps: IndexedStep<AgenticStep>[];
  collapsible: boolean;
  labelledBy?: string;
}) {
  return (
    <ol
      className={styles.steps}
      {...(labelledBy !== undefined ? { "aria-labelledby": labelledBy } : {})}
    >
      {steps.map(({ step, index }) => (
        <StepCard
          key={`${index}-${step.action}`}
          // The step's number in the plan as written, not its position in
          // this group: it is the number an operator quotes ("step 3 failed").
          index={index + 1}
          step={step}
          collapsible={collapsible}
        />
      ))}
    </ol>
  );
}

export function PlanView({ plan }: PlanViewProps) {
  const baseId = useId();
  const steps = plan?.steps ?? [];
  const rollback = plan?.rollback ?? [];
  const automatable = plan?.automatable === true;
  if (steps.length === 0 && rollback.length === 0) return null;

  const { grouped, now, followUp } = groupSteps(steps);
  const collapsible = steps.length > COLLAPSE_ABOVE;
  const headingId = `${baseId}-heading`;

  return (
    <section className={styles.plan} aria-labelledby={headingId}>
      <div className={styles.head}>
        <h3 className={styles.heading} id={headingId}>
          What to do
        </h3>
        {automatable ? (
          <AutomatableBadge />
        ) : (
          <span className={styles.manual} title={MANUAL_HINT}>
            Manual
          </span>
        )}
      </div>

      {steps.length === 0 ? null : grouped ? (
        <>
          {/* Two lists, not one list with dividers: "Now" is what on-call runs
              while the alert is live, "Follow-up" the work that stops it
              recurring, and a reader skipping straight to one should be able
              to. An empty group is simply absent. */}
          {now.length > 0 ? (
            <div className={styles.group}>
              <h4 className={styles.groupHeading} id={`${baseId}-now`}>
                Now
              </h4>
              <StepList steps={now} collapsible={collapsible} labelledBy={`${baseId}-now`} />
            </div>
          ) : null}
          {followUp.length > 0 ? (
            <div className={styles.group}>
              <h4 className={styles.groupHeading} id={`${baseId}-follow-up`}>
                Follow-up
              </h4>
              <StepList
                steps={followUp}
                collapsible={collapsible}
                labelledBy={`${baseId}-follow-up`}
              />
            </div>
          ) : null}
        </>
      ) : (
        <StepList steps={now} collapsible={collapsible} labelledBy={headingId} />
      )}

      {/* The undo is needed only when a step went wrong, so it waits folded
          under the plan rather than doubling its length on every read. */}
      {rollback.length > 0 ? (
        <Disclosure label={`Rollback · ${rollback.length}`} level={4}>
          <StepList
            steps={rollback.map((step, index) => ({ step, index }))}
            collapsible={rollback.length > COLLAPSE_ABOVE}
          />
        </Disclosure>
      ) : null}
    </section>
  );
}
