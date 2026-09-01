import { useLocation, useNavigate } from "@tanstack/react-router";
import { Button } from "@/shared/ui/Button";
import { EmptyState } from "@/shared/ui/EmptyState";
import { useAuth } from "@/lib/auth/store";
import { visibleNavItems } from "@/app/layout/nav-list";

/**
 * NotFound — the app shell's `notFoundComponent`, rendered inside the same
 * Outlet AppShell wraps every real page with. A stray/stale link (routine on
 * an ops tool that gets bookmarked and shared) used to render the bare text
 * "Not Found" on an empty canvas: no sidebar, no way back, no hint that ⌘K
 * jumps anywhere. This keeps the shell (nav, topbar, theme) intact and gives
 * an explicit way out, mirroring AccessDenied's shape.
 */
export function NotFound() {
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const { claims } = useAuth();
  const home = visibleNavItems(claims)[0];
  const homeTo = home?.to ?? "/web/alerts";
  const homeLabel = home?.label ?? "Alerts";
  return (
    <div style={{ padding: "var(--space-5)" }}>
      <EmptyState
        icon="search"
        title="Page not found"
        description={`There's no page at ${pathname}. It may have moved, or the link is stale — press ⌘K (Ctrl+K) to jump anywhere in the app.`}
        action={
          <Button variant="primary" onClick={() => void navigate({ to: homeTo })}>
            Back to {homeLabel}
          </Button>
        }
      />
    </div>
  );
}
