// The blast radius of one remediation step, as a chip.
//
// Low risk is the reassuring answer, so `riskTone` maps low → ok and high →
// critical (see enums.ts). Callers print it only above low: a plan whose
// every step says "Low risk" buries the one step that is not. The
// level is always spelled out — colour is never the only carrier.
import { Badge } from "@/shared/ui/Badge";
import { riskLabel, riskTone, type Risk } from "./enums";

export type RiskBadgeProps = {
  risk: Risk;
  className?: string;
};

export function RiskBadge({ risk, className }: RiskBadgeProps) {
  const label = riskLabel(risk);
  return (
    <Badge
      variant={riskTone(risk)}
      title={`${label} risk if this step is run`}
      {...(className !== undefined ? { className } : {})}
    >
      {`${label} risk`}
    </Badge>
  );
}
