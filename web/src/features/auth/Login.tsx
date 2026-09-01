import { useCallback, useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { Button } from "@/shared/ui/Button";
import { Input } from "@/shared/ui/Input";
import { Logo } from "@/shared/ui/Logo";
import { Icon } from "@/shared/icons/Icon";
import { describeError } from "@/lib/api/errorMessage";
import { authStore } from "@/lib/auth/store";
import { normalizeReturnTo } from "@/lib/auth/return-to";
import { firstLandingPath } from "@/app/layout/nav-list";
import {
  fetchLoginConfig,
  loginAnonymous,
  loginLdap,
  loginLocal,
  resolveTenantByKey,
  ssoStartUrl,
  type LoginBackend,
} from "./api";
import { MicrosoftLogo } from "./MicrosoftLogo";
import styles from "./Login.module.css";

type CredentialMethod = "local" | "ldap";

function isCredential(b: LoginBackend): boolean {
  return b.kind === "password" && (b.name === "local" || b.name === "ldap");
}

export function Login() {
  const navigate = useNavigate();
  const search = useSearch({ strict: false }) as unknown as {
    return_to?: string;
    key?: string;
    sso_error?: string;
  };
  // Post-login destination. `search.return_to` is already decoded by the
  // router (decoding it again would mangle encoded query state and throw on a
  // raw "%", white-screening this page), and must be a safe same-origin path.
  // When absent/unsafe, land on the user's first PERMITTED page — computed
  // after login below, since claims aren't available until then — so a user
  // without alert permissions isn't dropped onto the record-gated Alerts wall.
  const explicitReturnTo = normalizeReturnTo(search.return_to);

  const cfgQuery = useQuery({
    queryKey: ["login", "config"],
    queryFn: fetchLoginConfig,
    staleTime: 60_000,
  });
  const backends = useMemo<LoginBackend[]>(() => cfgQuery.data?.backends ?? [], [cfgQuery.data]);
  const tenants = cfgQuery.data?.tenants ?? [];

  const keyQuery = useQuery({
    queryKey: ["login", "tenant-by-key", search.key],
    queryFn: () => resolveTenantByKey(search.key!),
    enabled: !!search.key,
    staleTime: 60_000,
  });
  const lockedTenant = keyQuery.data ?? null;

  const credentialBackends = useMemo(() => backends.filter(isCredential), [backends]);

  // Primary credential method: prefer "local" if present, else "ldap", else null.
  const defaultPrimary: CredentialMethod | null = credentialBackends.some((b) => b.name === "local")
    ? "local"
    : credentialBackends.some((b) => b.name === "ldap")
      ? "ldap"
      : null;

  const [primary, setPrimary] = useState<CredentialMethod | null>(defaultPrimary);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [orgSel, setOrgSel] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(search.sso_error ?? null);

  const org = lockedTenant ? lockedTenant.id : tenants.length === 1 ? tenants[0]!.id : orgSel;
  // A multi-tenant deployment must not silently authenticate against the
  // default org: the org choice is mandatory (unless a ?key locks it or there's
  // exactly one tenant), and sign-in is blocked until it's made.
  const orgRequired = !lockedTenant && tenants.length > 1;
  const orgMissing = orgRequired && !orgSel;

  useEffect(() => {
    if (primary === null && defaultPrimary !== null) setPrimary(defaultPrimary);
  }, [defaultPrimary, primary]);

  // On a sign-in failure — including a bounce-back from a failed SSO redirect
  // (?sso_error=…) — pull keyboard + screen-reader focus to the error banner so
  // it's announced. A ref callback (not an effect keyed on `error`) is used so
  // the focus fires when the banner element actually mounts: on the SSO path
  // the error is set before the config finishes loading, so the banner isn't in
  // the DOM yet when an `error`-keyed effect would run.
  const focusErrorBanner = useCallback(
    (el: HTMLDivElement | null) => {
      if (el && error) el.focus();
    },
    [error],
  );

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (orgMissing) {
      setError("Select your organization to sign in.");
      return;
    }
    setError(null);
    setSubmitting(true);
    const orgSlug = org.trim() || undefined;
    try {
      const result =
        primary === "ldap"
          ? await loginLdap({ username, password, org: orgSlug })
          : await loginLocal({ username, password, org: orgSlug });
      authStore.getState().login(result.token, result.refreshToken);
      await navigate({ to: explicitReturnTo ?? firstLandingPath(authStore.getState().claims) });
    } catch (err) {
      setError(describeError(err, "Sign-in failed. Please try again.").summary);
    } finally {
      setSubmitting(false);
    }
  }

  async function handleAnonymous() {
    if (orgMissing) {
      setError("Select your organization to sign in.");
      return;
    }
    setError(null);
    setSubmitting(true);
    try {
      const result = await loginAnonymous(org.trim() || undefined);
      authStore.getState().login(result.token, result.refreshToken);
      await navigate({ to: explicitReturnTo ?? firstLandingPath(authStore.getState().claims) });
    } catch (err) {
      setError(describeError(err, "Sign-in failed. Please try again.").summary);
    } finally {
      setSubmitting(false);
    }
  }

  function startSso(name: string) {
    if (orgMissing) {
      setError("Select your organization to continue.");
      return;
    }
    const orgSlug = org.trim() || undefined;
    // Only forward an explicit, safe return_to; otherwise let the callback pick
    // the user's first permitted landing page (their claims aren't known yet).
    const opts: { org?: string; returnTo?: string } = {};
    if (explicitReturnTo) opts.returnTo = explicitReturnTo;
    if (orgSlug) opts.org = orgSlug;
    window.location.assign(ssoStartUrl(name, opts));
  }

  if (cfgQuery.isPending || (!!search.key && keyQuery.isPending)) {
    return (
      <div className={styles.page}>
        <div className={styles.card}>
          <div className={styles.brand}>
            <Logo />
            <span className={styles.tagline}>Alert Console</span>
          </div>
        </div>
      </div>
    );
  }

  if (backends.length === 0) {
    return (
      <div className={styles.page}>
        <div className={styles.card}>
          <div className={styles.brand}>
            <Logo />
            <span className={styles.tagline}>Alert Console</span>
          </div>
          <div className={styles.error} role="alert">
            No authentication backend is enabled on this server.
          </div>
        </div>
      </div>
    );
  }

  const tenantMatchHint =
    cfgQuery.data?.tenantMatchEnabled && !lockedTenant && tenants.length > 1 ? (
      <p className={styles.tenantMatchHint}>
        Your organisation is detected automatically — you do not need to select one.
      </p>
    ) : null;

  const orgField =
    !lockedTenant && tenants.length > 1 ? (
      <div className={`${styles.field} ${styles.orgField}`}>
        <label htmlFor="login-org" className={styles.label}>
          Organization
        </label>
        <select
          id="login-org"
          value={orgSel}
          onChange={(e) => setOrgSel(e.target.value)}
          className={styles.input}
          required
          aria-invalid={orgMissing || undefined}
          aria-describedby={orgMissing ? "login-org-hint" : undefined}
        >
          <option value="" disabled>
            Select your organization…
          </option>
          {tenants.map((t) => (
            <option key={t.id} value={t.id}>
              {t.display_name || t.id}
            </option>
          ))}
        </select>
        {orgMissing ? (
          <p id="login-org-hint" className={styles.hint}>
            Select your organization to sign in.
          </p>
        ) : null}
      </div>
    ) : null;

  const altBackends = backends.filter((b) => !(isCredential(b) && b.name === primary));

  return (
    <div className={styles.page}>
      <div className={styles.card}>
        <div className={styles.brand}>
          <Logo />
          <span className={styles.tagline}>Alert Console</span>
        </div>
        {lockedTenant ? (
          <p className={styles.tenantHeader}>
            Sign in to {lockedTenant.display_name || lockedTenant.id}
          </p>
        ) : null}

        {error ? (
          <div className={styles.error} role="alert" tabIndex={-1} ref={focusErrorBanner}>
            {error}
          </div>
        ) : null}

        {primary ? (
          <form
            className={styles.form}
            onSubmit={(e) => {
              void handleSubmit(e);
            }}
          >
            <div className={styles.field}>
              <label htmlFor="login-username" className={styles.label}>
                Username
              </label>
              <Input
                id="login-username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                autoComplete="username"
                spellCheck={false}
                autoCapitalize="none"
                required
              />
            </div>
            <div className={styles.field}>
              <label htmlFor="login-password" className={styles.label}>
                Password
              </label>
              <Input
                id="login-password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="current-password"
                required
              />
            </div>
            {tenantMatchHint}
            {orgField}
            <Button
              type="submit"
              variant="primary"
              loading={submitting}
              disabled={orgMissing}
              fullWidth
            >
              {primary === "ldap" ? "Sign in via LDAP" : "Sign in"}
            </Button>
          </form>
        ) : (
          <>
            {tenantMatchHint}
            {orgField}
          </>
        )}

        {altBackends.length > 0 ? (
          <div className={styles.ssoSection}>
            {primary ? <div className={styles.divider}>or continue with</div> : null}
            <div className={styles.ssoButtons}>
              {altBackends.map((b) => {
                if (b.kind === "redirect") {
                  return (
                    <Button
                      key={b.name}
                      variant="secondary"
                      fullWidth
                      disabled={submitting || orgMissing}
                      onClick={() => startSso(b.name)}
                    >
                      <span className={styles.ssoLabel}>
                        {b.icon === "microsoft" ? <MicrosoftLogo /> : <Icon name="lock" />}
                        {b.display_name || b.name}
                      </span>
                    </Button>
                  );
                }
                if (b.name === "anonymous") {
                  return (
                    <Button
                      key={b.name}
                      variant="secondary"
                      fullWidth
                      loading={submitting}
                      disabled={orgMissing}
                      onClick={() => {
                        void handleAnonymous();
                      }}
                    >
                      Continue as anonymous
                    </Button>
                  );
                }
                return (
                  <Button
                    key={b.name}
                    variant="ghost"
                    fullWidth
                    disabled={submitting}
                    onClick={() => {
                      setError(null);
                      setPrimary(b.name as CredentialMethod);
                    }}
                  >
                    {b.name === "ldap" ? "Sign in via LDAP" : "Sign in with a local account"}
                  </Button>
                );
              })}
            </div>
          </div>
        ) : null}
      </div>
    </div>
  );
}
