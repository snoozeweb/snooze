// Whether a remediation plan may be run without a human, as a chip.
//
// Sibling of ConfidenceBadge and RiskBadge, with one deliberate difference:
// it carries no severity colour. Confidence and risk are gradients — the
// badge's paint IS the reading. "Automatable" is a capability, true or absent,
// and the palette's colours are spoken for: blue belongs to severity-info,
// amber to interactive chrome, green to a high-confidence cause. A neutral
// chip beside a coloured one says "also true of this analysis" without
// claiming to be a fourth severity.
//
// Rendered only where the plan says `automatable: true`, so its presence is
// the signal; there is no "Manual" counterpart to print on every other row.
import { Badge } from "@/shared/ui/Badge";
import { Icon } from "@/shared/icons/Icon";

export type AutomatableBadgeProps = {
  className?: string;
};

export function AutomatableBadge({ className }: AutomatableBadgeProps) {
  return (
    <Badge
      variant="neutral"
      // What the flag actually asserts, in the words the editor uses when an
      // author sets it — the chip is a claim about blast radius, not a label.
      title="Every step is low risk and needs no judgement: safe to run unattended"
      {...(className !== undefined ? { className } : {})}
    >
      <Icon name="rotate-cw" size={12} />
      Automatable
    </Badge>
  );
}
