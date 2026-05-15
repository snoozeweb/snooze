import { api } from "@/lib/api/client";

type LoginCredentials = { username: string; password: string };

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
  const r = await api<LoginEnvelope>("POST", "/login/local", {
    body: creds,
    skipAuthHandling: true,
  });
  return unwrap(r);
}

export async function loginLdap(creds: LoginCredentials): Promise<LoginResult> {
  const r = await api<LoginEnvelope>("POST", "/login/ldap", {
    body: creds,
    skipAuthHandling: true,
  });
  return unwrap(r);
}

export async function loginAnonymous(): Promise<LoginResult> {
  const r = await api<LoginEnvelope>("POST", "/login/anonymous", {
    body: {},
    skipAuthHandling: true,
  });
  return unwrap(r);
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
