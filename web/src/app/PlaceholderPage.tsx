import { Card } from "@/shared/ui/Card";
import { EmptyState } from "@/shared/ui/EmptyState";
import type { IconName } from "@/shared/icons/icon-names";

export type PlaceholderPageProps = {
  title: string;
  icon: IconName;
  milestone: string;
};

export function PlaceholderPage({ title, icon, milestone }: PlaceholderPageProps) {
  return (
    <div style={{ padding: "var(--space-5)" }}>
      <Card padded>
        <EmptyState
          icon={icon}
          title={title}
          description={`This page lands in ${milestone}. The chrome around it is the real thing.`}
        />
      </Card>
    </div>
  );
}
