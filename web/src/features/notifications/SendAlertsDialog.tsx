import { useNavigate } from "@tanstack/react-router";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
} from "@/shared/ui/Dialog";
import { Button } from "@/shared/ui/Button";
import { Actions, Notifications } from "./api";
import styles from "./SendAlertsDialog.module.css";

export type SendAlertsDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

type StepCardProps = {
  step: number;
  title: string;
  description: string;
  count: number | null;
  ctaLabel: string;
  onNavigate: () => void;
};

function StepCard({ step, title, description, count, ctaLabel, onNavigate }: StepCardProps) {
  const resolved = count !== null;
  const ok = resolved && count > 0;
  const warn = resolved && count === 0;
  return (
    <div className={`${styles.card} ${ok ? styles.ok : warn ? styles.warn : ""}`}>
      <div className={styles.cardTop}>
        <span className={styles.stepLabel}>Step {step}</span>
        {resolved ? (
          <span className={`${styles.countBadge} ${ok ? styles.countOk : styles.countWarn}`}>
            {ok ? `✓ ${count} configured` : "⚠ None configured"}
          </span>
        ) : null}
      </div>
      <div className={styles.cardTitle}>{title}</div>
      <div className={styles.cardDesc}>{description}</div>
      <button type="button" className={styles.cta} onClick={onNavigate}>
        {ctaLabel} →
      </button>
    </div>
  );
}

export function SendAlertsDialog({ open, onOpenChange }: SendAlertsDialogProps) {
  const navigate = useNavigate();
  const actionList = Actions.useList({ limit: 1 }, { enabled: open });
  const notifList = Notifications.useList({ limit: 1 }, { enabled: open });

  const actionCount = actionList.data?.meta.total ?? null;
  const notifCount = notifList.data?.meta.total ?? null;

  function goTo(to: string, search?: Record<string, string>) {
    onOpenChange(false);
    void navigate({ to, ...(search ? { search } : {}) });
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogTitle>How to send alerts</DialogTitle>
        <DialogBody>
          <DialogDescription>
            Two steps to route a matching alert to an external service.
          </DialogDescription>
          <div className={styles.cards}>
            <StepCard
              step={1}
              title="Configure actions"
              description="An action sets up the delivery channel — email, Slack, webhook, Jira, Teams, …"
              count={actionCount}
              ctaLabel="Go to Actions"
              onNavigate={() => goTo("/web/notifications", { tab: "actions" })}
            />
            <StepCard
              step={2}
              title="Configure notifications"
              description="A notification links a filter condition to one or more actions."
              count={notifCount}
              ctaLabel="Go to Notifications"
              onNavigate={() => goTo("/web/notifications")}
            />
          </div>
        </DialogBody>
        <DialogFooter>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>
            Close
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
