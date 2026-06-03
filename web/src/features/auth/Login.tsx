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
  fetchLoginBackends,
  loginAnonymous,
  loginLdap,
  loginLocal,
  type LoginBackend,
} from "./api";
import styles from "./Login.module.css";

export function Login() {
  const navigate = useNavigate();
  const search = useSearch({ strict: false }) as unknown as { return_to?: string };
  const returnTo = search.return_to ? decodeURIComponent(search.return_to) : "/web/alerts";

  // Backends the server currently exposes. Falls back to ["local","ldap","anonymous"]
  // until the query resolves so a slow /api/v1/login fetch doesn't briefly render
  // an empty card. The actual UI is gated on the resolved value below.
  const backendsQuery = useQuery({
    queryKey: ["login", "backends"],
    queryFn: fetchLoginBackends,
    staleTime: 60_000,
  });
  const backends = useMemo<LoginBackend[]>(() => backendsQuery.data ?? [], [backendsQuery.data]);

  const [tab, setTab] = useState<LoginBackend | null>(null);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

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
    try {
      let result: { token: string; refreshToken: string | null };
      if (tab === "anonymous") {
        result = await loginAnonymous();
      } else if (tab === "ldap") {
        result = await loginLdap({ username, password });
      } else {
        result = await loginLocal({ username, password });
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

  // Loading state: nothing to render yet.
  if (backendsQuery.isPending) {
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

  return (
    <div className={styles.page}>
      <div className={styles.card}>
        <div className={styles.brand}>
          <Logo />
        </div>
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
