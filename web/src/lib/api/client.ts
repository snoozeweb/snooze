import { readRefreshToken, readToken, writeRefreshToken, writeToken } from "@/lib/auth/storage";

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    public detail: string,
    public traceId?: string,
  ) {
    super(detail || `HTTP ${status}`);
    this.name = "ApiError";
  }
}

type QueryValue = string | number | boolean | undefined;

export type ApiOptions = {
  query?: Record<string, QueryValue>;
  body?: unknown;
  signal?: AbortSignal;
  /**
   * When true, a 401 response does NOT invoke the unauthorized handler.
   * The login endpoints set this — we're already on the login page; a
   * 401 means "wrong credentials", not "session expired".
   */
  skipAuthHandling?: boolean;
  /**
   * When true, the 401 retry-via-refresh path is bypassed. The /refresh
   * and /logout endpoints set this so they never recurse into themselves.
   */
  skipRefreshHandling?: boolean;
};

export type Method = "GET" | "POST" | "PUT" | "PATCH" | "DELETE";

function buildUrl(path: string, query?: Record<string, QueryValue>): string {
  let url: string;
  if (path.startsWith("/api/")) {
    url = path;
  } else {
    url = `/api/v1${path.startsWith("/") ? path : `/${path}`}`;
  }
  if (query) {
    const params = new URLSearchParams();
    for (const [k, v] of Object.entries(query)) {
      if (v !== undefined) params.set(k, String(v));
    }
    const qs = params.toString();
    if (qs) url += `?${qs}`;
  }
  return url;
}

async function parseError(res: Response): Promise<ApiError> {
  let code = `http_${res.status}`;
  let detail = res.statusText || `HTTP ${res.status}`;
  let traceId: string | undefined;
  try {
    const ct = res.headers.get("Content-Type") ?? "";
    if (ct.includes("application/json")) {
      const body = (await res.json()) as {
        error?: {
          code?: string;
          message?: string;
          request_id?: string;
          trace_id?: string;
        };
        code?: string;
        detail?: string;
        trace_id?: string;
      };
      // Canonical server envelope is { error: { code, message, request_id,
      // trace_id } } (snoozetypes.ErrEnvelope). Older shapes used flat
      // { code, detail, trace_id } — keep that branch so the client stays
      // tolerant of a server we don't fully control.
      if (body.error && typeof body.error === "object") {
        if (typeof body.error.code === "string") code = body.error.code;
        if (typeof body.error.message === "string") detail = body.error.message;
        if (typeof body.error.trace_id === "string") traceId = body.error.trace_id;
        else if (typeof body.error.request_id === "string") traceId = body.error.request_id;
      } else {
        if (typeof body.code === "string") code = body.code;
        if (typeof body.detail === "string") detail = body.detail;
        if (typeof body.trace_id === "string") traceId = body.trace_id;
      }
    }
  } catch {
    // body wasn't JSON; keep the fallback fields
  }
  return new ApiError(res.status, code, detail, traceId);
}

let unauthorizedHandler: (() => void) | null = null;

export function setUnauthorizedHandler(handler: (() => void) | null): void {
  unauthorizedHandler = handler;
}

// In-flight refresh promise. Multiple concurrent 401s share one /refresh
// call so we never queue up parallel rotations of the same token (which
// would race and revoke each other).
let refreshInFlight: Promise<string | null> | null = null;

type RefreshEnvelope = {
  token: string;
  refresh_token?: string;
};

async function rotateTokens(): Promise<string | null> {
  const stored = readRefreshToken();
  if (!stored) return null;
  if (refreshInFlight) return refreshInFlight;
  refreshInFlight = (async () => {
    try {
      const res = await fetch("/api/v1/login/refresh", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ refresh_token: stored }),
      });
      if (!res.ok) return null;
      const env = (await res.json()) as RefreshEnvelope;
      if (!env.token) return null;
      writeToken(env.token);
      writeRefreshToken(env.refresh_token ?? null);
      return env.token;
    } catch {
      return null;
    } finally {
      refreshInFlight = null;
    }
  })();
  return refreshInFlight;
}

async function doFetch(
  method: Method,
  url: string,
  body: unknown,
  signal: AbortSignal | undefined,
  token: string | null,
): Promise<Response> {
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (token) headers["Authorization"] = `Bearer ${token}`;
  const init: RequestInit = { method, headers };
  if (body !== undefined) init.body = JSON.stringify(body);
  if (signal) init.signal = signal;
  return fetch(url, init);
}

export async function api<T = unknown>(
  method: Method,
  path: string,
  opts: ApiOptions = {},
): Promise<T> {
  const url = buildUrl(path, opts.query);
  let token = readToken();
  let res = await doFetch(method, url, opts.body, opts.signal, token);

  // Transparent refresh: if the access token was rejected but we still
  // have a refresh token, attempt to rotate once and retry the original
  // request. The refresh endpoint itself sets skipRefreshHandling so it
  // never recurses into this branch.
  if (res.status === 401 && !opts.skipRefreshHandling && readRefreshToken()) {
    const fresh = await rotateTokens();
    if (fresh) {
      token = fresh;
      res = await doFetch(method, url, opts.body, opts.signal, token);
    }
  }

  if (res.status === 401) {
    const parsed = await parseError(res);
    // parseError fills code with "http_401" when no JSON envelope was
    // present. Map that to the friendlier "unauthorized" code so existing
    // callers keep working.
    const err =
      parsed.code === "http_401"
        ? new ApiError(401, "unauthorized", parsed.detail || "Not authenticated", parsed.traceId)
        : parsed;
    if (!opts.skipAuthHandling) {
      unauthorizedHandler?.();
    }
    throw err;
  }
  if (!res.ok) {
    throw await parseError(res);
  }
  if (res.status === 204) {
    return undefined as T;
  }
  return (await res.json()) as T;
}
