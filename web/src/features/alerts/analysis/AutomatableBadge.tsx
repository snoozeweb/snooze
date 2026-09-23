// Whether a remediation plan may be run without a human, as a chip.
//
// It carries no state colour, and no glow (see the stylesheet): "Automatable" is a capability, true or absent,
// and the palette's hues are spoken for — blue belongs to severity-info, amber
// to interactive chrome, green and amber again to the plan's verdict. A
// neutral chip says "also true of this analysis" without claiming to be a
// severity. The icon is a bolt ("runs by itself"); it used to be rotate-cw,
// which every operator read as "refresh".
//
// Only rendered where the plan says `automatable: true`. Surfaces with room
// for the opposite (the Analysis tab's "What to do" heading) print a quiet
// "Manual" beside it themselves; row-dense ones (the dashboard) let the
// chip's absence speak.
import { Icon } from "@/shared/icons/Icon";
import styles from "./AutomatableBadge.module.css";

export type AutomatableBadgeProps = {
  className?: string;
};

export function AutomatableBadge({ className }: AutomatableBadgeProps) {
  return (
    <span
      className={[styles.neutral, className].filter(Boolean).join(" ")}
      // What the flag actually asserts, in the words the editor uses when an
      // author sets it — the chip is a claim about blast radius, not a label.
      title="Every step is low risk and needs no judgement: safe to run unattended"
    >
      <Icon name="zap" size={12} />
      Automatable
    </span>
  );
}
