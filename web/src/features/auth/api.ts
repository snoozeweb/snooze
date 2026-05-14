import { ApiError } from "@/lib/api/client";

type LoginCredentials = { username: string; password: string };
type LoginResponse = { token: string };

async function loginPost(path: string, body: unknown): Promise<string> {
  const res = await fetch(`/api/v1${path}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  let json: Record<string, unknown> = {};
  try {
    const ct = res.headers.get("Content-Type") ?? "";
    if (ct.includes("application/json")) {
      json = (await res.json()) as Record<string, unknown>;
    }
  } catch {
    // ignore parse failures
  }
  if (!res.ok) {
    const code = typeof json.code === "string" ? json.code : `http_${res.status}`;
    const detail =
      typeof json.detail === "string" ? json.detail : res.statusText || `HTTP ${res.status}`;
    const traceId = typeof json.trace_id === "string" ? json.trace_id : undefined;
    throw new ApiError(res.status, code, detail, traceId);
  }
  return (json as unknown as LoginResponse).token;
}

export async function loginLocal(creds: LoginCredentials): Promise<string> {
  return loginPost("/login/local", creds);
}

export async function loginLdap(creds: LoginCredentials): Promise<string> {
  return loginPost("/login/ldap", creds);
}

export async function loginAnonymous(): Promise<string> {
  return loginPost("/login/anonymous", {});
}
