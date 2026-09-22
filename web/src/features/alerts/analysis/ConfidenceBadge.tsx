// How much to trust a root cause, as a chip.
//
// The tone comes from `enums.ts` (high = ok, medium = warning, low = critical)
// and lands on Badge's severity-token variants, so the chip re-themes with the
// rest of the app. The level is always spelled out in the text — colour is
// never the only carrier of the meaning.
import { Badge } from "@/shared/ui/Badge";
import { confidenceLabel, confidenceTone, type Confidence } from "./enums";

export type ConfidenceBadgeProps = {
  confidence: Confidence;
  className?: string;
};

export function ConfidenceBadge({ confidence, className }: ConfidenceBadgeProps) {
  const label = confidenceLabel(confidence);
  return (
    <Badge
      variant={confidenceTone(confidence)}
      title={`${label} confidence in this root cause`}
      {...(className !== undefined ? { className } : {})}
    >
      {`${label} confidence`}
    </Badge>
  );
}
