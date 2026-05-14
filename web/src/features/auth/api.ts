import { api } from "@/lib/api/client";

type LoginCredentials = { username: string; password: string };
type LoginResponse = { token: string };

export async function loginLocal(creds: LoginCredentials): Promise<string> {
  const r = await api<LoginResponse>("POST", "/login/local", {
    body: creds,
    skipAuthHandling: true,
  });
  return r.token;
}

export async function loginLdap(creds: LoginCredentials): Promise<string> {
  const r = await api<LoginResponse>("POST", "/login/ldap", {
    body: creds,
    skipAuthHandling: true,
  });
  return r.token;
}

export async function loginAnonymous(): Promise<string> {
  const r = await api<LoginResponse>("POST", "/login/anonymous", {
    body: {},
    skipAuthHandling: true,
  });
  return r.token;
}
