import { useNavigate } from "@tanstack/react-router";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { Card } from "@/shared/ui/Card";
import { EmptyState } from "@/shared/ui/EmptyState";
import { secondsUntilExpiry } from "@/lib/auth/jwt";
import { useAuth } from "@/lib/auth/store";
import styles from "./Profile.module.css";

function formatExpiry(seconds: number): string {
  if (!Number.isFinite(seconds)) return "never";
  if (seconds <= 0) return "expired";
  if (seconds < 60) return `${Math.floor(seconds)}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`;
}

export function Profile() {
  const { claims, logout } = useAuth();
  const navigate = useNavigate();

  if (!claims) {
    return (
      <div className={styles.page}>
        <Card padded>
          <EmptyState icon="lock" title="Not signed in" />
        </Card>
      </div>
    );
  }

  const perms = Array.isArray(claims.permissions) ? claims.permissions : [];
  const expiry = secondsUntilExpiry(claims);

  function handleLogout() {
    logout();
    void navigate({ to: "/web/login" as string });
  }

  return (
    <div className={styles.page}>
      <h1 className={styles.title}>Profile</h1>
      <Card padded>
        <div style={{ display: "flex", flexDirection: "column", gap: "var(--space-3)" }}>
          <div className={styles.row}>
            <span className={styles.label}>Username</span>
            <span className={styles.value}>{claims.sub ?? "—"}</span>
          </div>
          <div className={styles.row}>
            <span className={styles.label}>Session</span>
            <span className={styles.value}>expires in {formatExpiry(expiry)}</span>
          </div>
          <div className={styles.row}>
            <span className={styles.label}>Permissions</span>
            <div className={styles.perms}>
              {perms.length === 0 ? (
                <Badge variant="muted">none</Badge>
              ) : (
                perms.map((p) => (
                  <Badge key={p} variant="info">
                    {p}
                  </Badge>
                ))
              )}
            </div>
          </div>
        </div>
      </Card>
      <div>
        <Button variant="danger" leadingIcon="lock" onClick={handleLogout}>
          Log out
        </Button>
      </div>
    </div>
  );
}
