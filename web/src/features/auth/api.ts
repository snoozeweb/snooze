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

// A login backend descriptor as returned by GET /api/v1/login.
// kind: "password" => local/ldap (credential form) or anonymous (one-click);
//       "redirect"  => OIDC/OAuth (button -> /start).
export type BackendKind = "password" | "redirect";
export type LoginBackend = {
  name: string;
  kind: BackendKind;
  display_name?: string;
  icon?: string;
};

const KNOWN_PASSWORD = new Set(["local", "ldap", "anonymous"]);

// parseBackends accepts the descriptor list and, defensively, the legacy
// string list a pre-upgrade server might still return.
export function parseBackends(raw: unknown): LoginBackend[] {
  if (!Array.isArray(raw)) return [];
  const out: LoginBackend[] = [];
  for (const entry of raw) {
    if (typeof entry === "string") {
      if (KNOWN_PASSWORD.has(entry)) out.push({ name: entry, kind: "password" });
      continue;
    }
    if (entry && typeof entry === "object" && typeof (entry as { name?: unknown }).name === "string") {
      const e = entry as { name: string; kind?: string; display_name?: string; icon?: string };
      const b: LoginBackend = { name: e.name, kind: e.kind === "redirect" ? "redirect" : "password" };
      if (e.display_name) b.display_name = e.display_name;
      if (e.icon) b.icon = e.icon;
      out.push(b);
    }
  }
  return out;
}

// ssoStartUrl builds the absolute browser-navigation URL that begins an OIDC
// redirect login. It is NOT fetched — the SPA sets window.location to it.
export function ssoStartUrl(name: string, opts: { org?: string; returnTo?: string }): string {
  const params = new URLSearchParams();
  if (opts.org) params.set("org", opts.org);
  if (opts.returnTo) params.set("return_to", opts.returnTo);
  const qs = params.toString();
  return `/api/v1/login/${encodeURIComponent(name)}/start${qs ? `?${qs}` : ""}`;
}

// PublicTenant is the minimal tenant shape the public login index returns.
// The server never includes login_key in the public list.
export type PublicTenant = { id: string; display_name: string };

// LoginConfig bundles the backends list and the public tenant list returned
// by GET /api/v1/login so callers only need one request on page load.
export type LoginConfig = { backends: LoginBackend[]; tenants: PublicTenant[] };

// fetchLoginConfig fetches both the auth backends and the public tenant list
// from GET /api/v1/login in a single call.
export async function fetchLoginConfig(): Promise<LoginConfig> {
  const r = await api<{ data?: { backends?: unknown; tenants?: PublicTenant[] } }>("GET", "/login", {
    skipAuthHandling: true,
  });
  const backends = parseBackends(r.data?.backends);
  const tenants = (r.data?.tenants ?? []).filter(
    (t): t is PublicTenant => !!t && typeof t.id === "string",
  );
  return { backends, tenants };
}

// resolveTenantByKey resolves an opaque per-tenant login key to its
// {id, display_name}. Returns null on any error (unknown key, suspended
// tenant, network error) — never reveals whether a tenant exists.
export async function resolveTenantByKey(key: string): Promise<PublicTenant | null> {
  try {
    const r = await api<{ data?: PublicTenant }>("GET", "/login/tenant", {
      query: { key },
      skipAuthHandling: true,
    });
    return r.data ?? null;
  } catch {
    return null; // never reveal whether a key/tenant exists
  }
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
