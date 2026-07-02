import { useNavigate } from "@tanstack/react-router";
import { Button } from "@/shared/ui/Button";
import { EmptyState } from "@/shared/ui/EmptyState";

/**
 * AccessDenied — the app's single, explanatory "you don't have permission"
 * state. Used as the `fallback` for RequirePerm on permission-gated routes so a
 * user who follows a bookmarked/shared URL they lack rights to gets a clear
 * message (naming the missing permission) and a way back, instead of a broken
 * or silently-empty page.
 */
export function AccessDenied({
  perms,
  homeTo = "/web/alerts",
  homeLabel = "Alerts",
}: {
  perms?: readonly string[];
  /** Where the escape CTA goes — a page the user can actually see (the caller
   *  passes their first permitted destination) so it's never a dead-end back
   *  into another wall. */
  homeTo?: string;
  homeLabel?: string;
}) {
  const navigate = useNavigate();
  const need = perms && perms.length > 0 ? perms.join(" or ") : null;
  return (
    <div style={{ padding: "var(--space-5)" }}>
      <EmptyState
        icon="lock"
        title="Access denied"
        description={
          need
            ? `You need the ${need} permission to view this page. Ask an administrator to grant it.`
            : "You don't have permission to view this page."
        }
        action={
          <Button variant="primary" onClick={() => void navigate({ to: homeTo })}>
            Go to {homeLabel}
          </Button>
        }
      />
    </div>
  );
}
