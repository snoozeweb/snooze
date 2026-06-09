import { useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { Card } from "@/shared/ui/Card";
import { EmptyState } from "@/shared/ui/EmptyState";
import { Input } from "@/shared/ui/Input";
import { toast } from "@/shared/ui/toast/useToast";
import { ApiError } from "@/lib/api/client";
import { permissionBadgeVariant } from "@/lib/format/permission-color";
import { secondsUntilExpiry } from "@/lib/auth/jwt";
import { useAuth } from "@/lib/auth/store";
import { changeOwnPassword } from "./api";
import styles from "./Profile.module.css";

function formatExpiry(seconds: number): string {
  if (!Number.isFinite(seconds)) return "never";
  if (seconds <= 0) return "expired";
  if (seconds < 60) return `${Math.floor(seconds)}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`;
}

// ChangePasswordForm renders the self-service password-change widget.
// Gated by the caller's `method` claim — the server enforces the same
// rule, but the UI hides the form for non-local accounts so users with
// LDAP credentials don't see a control that would only ever 403.
function ChangePasswordForm() {
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const mismatch = confirm !== "" && confirm !== next;
  const canSubmit = current !== "" && next !== "" && !mismatch && !submitting;

  async function submit() {
    if (!canSubmit) return;
    setSubmitting(true);
    try {
      await changeOwnPassword({ currentPassword: current, password: next });
      toast.success("Password updated");
      setCurrent("");
      setNext("");
      setConfirm("");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.detail : "Password change failed");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form
      className={styles.formStack}
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
    >
      <h2 className={styles.sectionTitle}>Change password</h2>
      <div className={styles.formRow}>
        <label className={styles.formLabel} htmlFor="profile-current-password">
          Current password
        </label>
        <div className={styles.formInput}>
          <Input
            id="profile-current-password"
            type="password"
            autoComplete="current-password"
            value={current}
            onChange={(e) => setCurrent(e.target.value)}
          />
        </div>
      </div>
      <div className={styles.formRow}>
        <label className={styles.formLabel} htmlFor="profile-new-password">
          New password
        </label>
        <div className={styles.formInput}>
          <Input
            id="profile-new-password"
            type="password"
            autoComplete="new-password"
            value={next}
            onChange={(e) => setNext(e.target.value)}
          />
        </div>
      </div>
      <div className={styles.formRow}>
        <label className={styles.formLabel} htmlFor="profile-confirm-password">
          Confirm new password
        </label>
        <div className={styles.formInput}>
          <Input
            id="profile-confirm-password"
            type="password"
            autoComplete="new-password"
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
            invalid={mismatch}
          />
        </div>
      </div>
      <div className={styles.formActions}>
        <Button type="submit" variant="primary" disabled={!canSubmit} loading={submitting}>
          Update password
        </Button>
      </div>
    </form>
  );
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
  // The "method" claim drives whether self-service password change is
  // available — only local-method accounts have a server-side hash to
  // rotate. Cast through unknown because JwtClaims keeps `method` in the
  // open-ended index signature.
  const method = typeof claims.method === "string" ? claims.method : undefined;

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
                  <Badge key={p} variant={permissionBadgeVariant(p)}>
                    {p}
                  </Badge>
                ))
              )}
            </div>
          </div>
        </div>
      </Card>
      {method === "local" ? (
        <Card padded>
          <ChangePasswordForm />
        </Card>
      ) : null}
      <div>
        <Button variant="danger" leadingIcon="lock" onClick={handleLogout}>
          Log out
        </Button>
      </div>
    </div>
  );
}
