import { api } from "@/lib/api/client";

type LoginCredentials = { username: string; password: string };
type LoginResponse = { token: string };

export async function loginLocal(creds: LoginCredentials): Promise<string> {
  const r = await api<LoginResponse>("POST", "/login/local", { body: creds });
  return r.token;
}

export async function loginLdap(creds: LoginCredentials): Promise<string> {
  const r = await api<LoginResponse>("POST", "/login/ldap", { body: creds });
  return r.token;
}

export async function loginAnonymous(): Promise<string> {
  const r = await api<LoginResponse>("POST", "/login/anonymous", { body: {} });
  return r.token;
}
