import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { Button } from "@/shared/ui/Button";
import { Input } from "@/shared/ui/Input";
import { Logo } from "@/shared/ui/Logo";
import { TabList, TabPanel, TabTrigger, Tabs } from "@/shared/ui/Tabs";
import { ApiError } from "@/lib/api/client";
import { authStore } from "@/lib/auth/store";
import {
  fetchLoginConfig,
  loginAnonymous,
  loginLdap,
  loginLocal,
  resolveTenantByKey,
  type LoginBackend,
} from "./api";
import styles from "./Login.module.css";

export function Login() {
  const navigate = useNavigate();
  const search = useSearch({ strict: false }) as unknown as {
    return_to?: string;
    key?: string;
  };
  const returnTo = search.return_to ? decodeURIComponent(search.return_to) : "/web/alerts";

  // Fetch backends + public tenant list in a single call.
  const cfgQuery = useQuery({
    queryKey: ["login", "config"],
    queryFn: fetchLoginConfig,
    staleTime: 60_000,
  });
  const backends = useMemo<LoginBackend[]>(() => cfgQuery.data?.backends ?? [], [cfgQuery.data]);
  const tenants = cfgQuery.data?.tenants ?? [];

  // Resolve an opaque ?key= param to a tenant (SaaS per-tenant login link).
  const keyQuery = useQuery({
    queryKey: ["login", "tenant-by-key", search.key],
    queryFn: () => resolveTenantByKey(search.key!),
    enabled: !!search.key,
    staleTime: 60_000,
  });
  const lockedTenant = keyQuery.data ?? null;

  const [tab, setTab] = useState<LoginBackend | null>(null);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  // orgSel is used only when the picker is visible (tenants.length > 1, no locked tenant).
  const [orgSel, setOrgSel] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Derive the effective org:
  // 1. Locked tenant (from ?key=) → use its id.
  // 2. Exactly one listed tenant → use it implicitly.
  // 3. More than one listed tenant → use the picker value (orgSel).
  // 4. No tenants → empty string (server defaults to "default" tenant).
  const org = lockedTenant
    ? lockedTenant.id
    : tenants.length === 1
      ? tenants[0]!.id
      : orgSel;

  // Default-select the first advertised backend. Re-runs when the backend
  // list resolves; only sets when the previously-selected tab is no longer
  // valid (e.g. server reconfig between renders).
  useEffect(() => {
    if (backends.length === 0) return;
    if (tab === null || !backends.includes(tab)) {
      setTab(backends[0] ?? null);
    }
  }, [backends, tab]);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    const orgSlug = org.trim() || undefined;
    try {
      let result: { token: string; refreshToken: string | null };
      if (tab === "anonymous") {
        result = await loginAnonymous(orgSlug);
      } else if (tab === "ldap") {
        result = await loginLdap({ username, password, org: orgSlug });
      } else {
        result = await loginLocal({ username, password, org: orgSlug });
      }
      authStore.getState().login(result.token, result.refreshToken);
      await navigate({ to: returnTo });
    } catch (e) {
      if (e instanceof ApiError) {
        setError(e.detail);
      } else {
        setError("Sign-in failed. Please try again.");
      }
    } finally {
      setSubmitting(false);
    }
  }

  // Loading state: nothing to render yet. When a ?key= is present we also wait
  // for the tenant resolve so the locked-org view doesn't flash the picker /
  // default first.
  if (cfgQuery.isPending || (!!search.key && keyQuery.isPending)) {
    return (
      <div className={styles.page}>
        <div className={styles.card}>
          <div className={styles.brand}>
            <Logo />
          </div>
        </div>
      </div>
    );
  }

  // No backends advertised → the operator has turned everything off.
  if (backends.length === 0) {
    return (
      <div className={styles.page}>
        <div className={styles.card}>
          <div className={styles.brand}>
            <Logo />
          </div>
          <div className={styles.error} role="alert">
            No authentication backend is enabled on this server.
          </div>
        </div>
      </div>
    );
  }

  /** Organization picker — shown only when there are >1 tenants and no locked tenant. */
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
      </div>
    ) : null;

  return (
    <div className={styles.page}>
      <div className={styles.card}>
        <div className={styles.brand}>
          <Logo />
        </div>
        {lockedTenant ? (
          <p className={styles.tenantHeader}>
            Sign in to {lockedTenant.display_name || lockedTenant.id}
          </p>
        ) : null}
        <Tabs
          value={tab ?? backends[0] ?? "local"}
          onValueChange={(v) => {
            setTab(v as LoginBackend);
            setError(null);
          }}
        >
          <TabList>
            {backends.includes("local") ? <TabTrigger value="local">Local</TabTrigger> : null}
            {backends.includes("ldap") ? <TabTrigger value="ldap">LDAP</TabTrigger> : null}
            {backends.includes("anonymous") ? (
              <TabTrigger value="anonymous">Anonymous</TabTrigger>
            ) : null}
          </TabList>
          {backends.includes("local") ? (
            <TabPanel value="local">
              <form
                className={styles.form}
                onSubmit={(e) => {
                  void handleSubmit(e);
                }}
              >
                <div className={styles.field}>
                  <label htmlFor="login-username-local" className={styles.label}>
                    Username
                  </label>
                  <Input
                    id="login-username-local"
                    value={username}
                    onChange={(e) => setUsername(e.target.value)}
                    autoComplete="username"
                    required
                  />
                </div>
                <div className={styles.field}>
                  <label htmlFor="login-password-local" className={styles.label}>
                    Password
                  </label>
                  <Input
                    id="login-password-local"
                    type="password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    autoComplete="current-password"
                    required
                  />
                </div>
                {orgField}
                {error ? (
                  <div className={styles.error} role="alert">
                    {error}
                  </div>
                ) : null}
                <Button type="submit" variant="primary" loading={submitting} fullWidth>
                  Sign in
                </Button>
              </form>
            </TabPanel>
          ) : null}
          {backends.includes("ldap") ? (
            <TabPanel value="ldap">
              <form
                className={styles.form}
                onSubmit={(e) => {
                  void handleSubmit(e);
                }}
              >
                <div className={styles.field}>
                  <label htmlFor="login-username-ldap" className={styles.label}>
                    Username
                  </label>
                  <Input
                    id="login-username-ldap"
                    value={username}
                    onChange={(e) => setUsername(e.target.value)}
                    autoComplete="username"
                    required
                  />
                </div>
                <div className={styles.field}>
                  <label htmlFor="login-password-ldap" className={styles.label}>
                    Password
                  </label>
                  <Input
                    id="login-password-ldap"
                    type="password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    autoComplete="current-password"
                    required
                  />
                </div>
                {orgField}
                {error ? (
                  <div className={styles.error} role="alert">
                    {error}
                  </div>
                ) : null}
                <Button type="submit" variant="primary" loading={submitting} fullWidth>
                  Sign in via LDAP
                </Button>
              </form>
            </TabPanel>
          ) : null}
          {backends.includes("anonymous") ? (
            <TabPanel value="anonymous">
              <form
                className={styles.form}
                onSubmit={(e) => {
                  void handleSubmit(e);
                }}
              >
                <p className={styles.anonymous}>
                  Sign in without credentials. Some servers disable this.
                </p>
                {orgField}
                {error ? (
                  <div className={styles.error} role="alert">
                    {error}
                  </div>
                ) : null}
                <Button type="submit" variant="primary" loading={submitting} fullWidth>
                  Continue as anonymous
                </Button>
              </form>
            </TabPanel>
          ) : null}
        </Tabs>
      </div>
    </div>
  );
}
