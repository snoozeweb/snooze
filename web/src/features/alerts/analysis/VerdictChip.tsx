// The plan's verdict — act, watch, or already recovered — as the first thing
// an analysis says.
//
// This is the one analysis mark that takes a state colour, because it IS a
// state: "Action required" borrows the warning hue the product uses for
// "needs attention", "Self-resolved" the ok hue, "Monitoring" the info hue.
// An icon rides with each so the verdict never rests on colour alone.
import type { IconName } from "@/shared/icons/icon-names";
import { Icon } from "@/shared/icons/Icon";
import { planStatusHint, planStatusLabel, type PlanStatus } from "./verdict";
import styles from "./VerdictChip.module.css";

const ICONS: Record<PlanStatus, IconName> = {
  action_required: "alert-triangle",
  monitoring: "eye",
  self_resolved: "check-circle",
};

export type VerdictChipProps = {
  status: PlanStatus;
  className?: string;
};

export function VerdictChip({ status, className }: VerdictChipProps) {
  return (
    <span
      className={[styles.chip, className].filter(Boolean).join(" ")}
      data-status={status}
      title={planStatusHint(status)}
    >
      <Icon name={ICONS[status]} size={12} />
      {planStatusLabel(status)}
    </span>
  );
}
