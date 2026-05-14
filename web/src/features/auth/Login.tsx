import { useState } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { Button } from "@/shared/ui/Button";
import { Icon } from "@/shared/icons/Icon";
import { Input } from "@/shared/ui/Input";
import { TabList, TabPanel, TabTrigger, Tabs } from "@/shared/ui/Tabs";
import { ApiError } from "@/lib/api/client";
import { authStore } from "@/lib/auth/store";
import { loginAnonymous, loginLdap, loginLocal } from "./api";
import styles from "./Login.module.css";

type Method = "local" | "ldap" | "anonymous";

export function Login() {
  const navigate = useNavigate();
  const search = useSearch({ strict: false }) as unknown as { return_to?: string };
  const returnTo = search.return_to ? decodeURIComponent(search.return_to) : "/web/alerts";

  const [tab, setTab] = useState<Method>("local");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      let token: string;
      if (tab === "anonymous") {
        token = await loginAnonymous();
      } else if (tab === "ldap") {
        token = await loginLdap({ username, password });
      } else {
        token = await loginLocal({ username, password });
      }
      authStore.getState().login(token);
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

  return (
    <div className={styles.page}>
      <div className={styles.card}>
        <div className={styles.brand}>
          <Icon name="bell-off" size={24} />
          <span>Snooze</span>
        </div>
        <Tabs
          value={tab}
          onValueChange={(v) => {
            setTab(v as Method);
            setError(null);
          }}
        >
          <TabList>
            <TabTrigger value="local">Local</TabTrigger>
            <TabTrigger value="ldap">LDAP</TabTrigger>
            <TabTrigger value="anonymous">Anonymous</TabTrigger>
          </TabList>
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
        </Tabs>
      </div>
    </div>
  );
}
