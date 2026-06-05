import { api } from "@/lib/api/client";

// org is `string | undefined` (not just optional) so call sites can pass an
// already-resolved `org.trim() || undefined` under the project's
// exactOptionalPropertyTypes setting.
type LoginCredentials = { username: string; password: string; org?: string | undefined };

// LoginEnvelope mirrors the LoginResponse component in api/openapi.yaml.
// refresh_token / refresh_expires_at are optional so a server that doesn't
// issue refresh tokens (e.g. an older deployment) still works.
type LoginEnvelope = {
  token: string;
  expires_at?: string;
  refresh_token?: string;
  refresh_expires_at?: string;
  method?: string;
};

export type LoginResult = {
  token: string;
  refreshToken: string | null;
};

function unwrap(env: LoginEnvelope): LoginResult {
  return {
    token: env.token,
    refreshToken: env.refresh_token ?? null,
  };
}

export async function loginLocal(creds: LoginCredentials): Promise<LoginResult> {
  const body: Record<string, string> = { username: creds.username, password: creds.password };
  if (creds.org) body["org"] = creds.org;
  const r = await api<LoginEnvelope>("POST", "/login/local", {
    body,
    skipAuthHandling: true,
  });
  return unwrap(r);
}

export async function loginLdap(creds: LoginCredentials): Promise<LoginResult> {
  const body: Record<string, string> = { username: creds.username, password: creds.password };
  if (creds.org) body["org"] = creds.org;
  const r = await api<LoginEnvelope>("POST", "/login/ldap", {
    body,
    skipAuthHandling: true,
  });
  return unwrap(r);
}

export async function loginAnonymous(org?: string): Promise<LoginResult> {
  const body: Record<string, string> = {};
  if (org) body["org"] = org;
  const r = await api<LoginEnvelope>("POST", "/login/anonymous", {
    body,
    skipAuthHandling: true,
  });
  return unwrap(r);
}

// fetchLoginBackends returns the names of auth backends the server currently
// advertises. The server filters out disabled providers (general.local_enabled,
// ldap.enabled, general.anonymous_enabled), so the Login UI just renders one
// tab per name in the response.
export type LoginBackend = "local" | "ldap" | "anonymous";

export async function fetchLoginBackends(): Promise<LoginBackend[]> {
  const r = await api<{ data?: { backends?: string[] } }>("GET", "/login", {
    skipAuthHandling: true,
  });
  const raw = r.data?.backends ?? [];
  return raw.filter((b): b is LoginBackend => b === "local" || b === "ldap" || b === "anonymous");
}

// postRefresh exchanges an opaque refresh token for a new (access, refresh)
// pair. The API client uses this transparently on 401; UI code should not
// call it directly.
export async function postRefresh(refreshToken: string): Promise<LoginResult> {
  const r = await api<LoginEnvelope>("POST", "/login/refresh", {
    body: { refresh_token: refreshToken },
    skipAuthHandling: true,
    skipRefreshHandling: true,
  });
  return unwrap(r);
}

// changeOwnPassword replaces the authenticated user's password. The server
// re-verifies the current password through the same code path as
// /login/local, so a wrong current password surfaces as 401 with the same
// generic "invalid credentials" envelope the login flow returns.
//
// Only callable when claims.method === "local"; LDAP / anonymous accounts
// 403 server-side. The Profile UI gates the form on that condition too.
export async function changeOwnPassword(input: {
  currentPassword: string;
  password: string;
}): Promise<void> {
  await api<void>("POST", "/user/me/password", {
    body: {
      current_password: input.currentPassword,
      password: input.password,
    },
  });
}

// postLogout best-effort revokes the supplied refresh token. Server always
// returns 204 — failures are swallowed so logging out never blocks UI flow.
export async function postLogout(refreshToken: string | null): Promise<void> {
  if (!refreshToken) return;
  try {
    await api<void>("POST", "/login/logout", {
      body: { refresh_token: refreshToken },
      skipAuthHandling: true,
      skipRefreshHandling: true,
    });
  } catch {
    // Logout is fire-and-forget. The client clears local state regardless.
  }
}
