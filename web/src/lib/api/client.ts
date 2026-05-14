import { readToken } from "@/lib/auth/storage";

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
        code?: string;
        detail?: string;
        trace_id?: string;
      };
      if (typeof body.code === "string") code = body.code;
      if (typeof body.detail === "string") detail = body.detail;
      if (typeof body.trace_id === "string") traceId = body.trace_id;
    }
  } catch {
    // body wasn't JSON; keep the fallback fields
  }
  return new ApiError(res.status, code, detail, traceId);
}

export async function api<T = unknown>(
  method: Method,
  path: string,
  opts: ApiOptions = {},
): Promise<T> {
  const url = buildUrl(path, opts.query);
  const token = readToken();
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  if (token) headers["Authorization"] = `Bearer ${token}`;

  const init: RequestInit = {
    method,
    headers,
  };
  if (opts.body !== undefined) init.body = JSON.stringify(opts.body);
  if (opts.signal) init.signal = opts.signal;

  const res = await fetch(url, init);

  if (res.status === 401) {
    throw new ApiError(401, "unauthorized", "Not authenticated");
  }
  if (!res.ok) {
    throw await parseError(res);
  }
  if (res.status === 204) {
    return undefined as T;
  }
  return (await res.json()) as T;
}
