import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, api, setUnauthorizedHandler } from "./client";
import { writeToken } from "@/lib/auth/storage";

type FetchHandler = (input: RequestInfo | URL, init?: RequestInit) => Response | Promise<Response>;

function urlToString(input: RequestInfo | URL): string {
  if (typeof input === "string") return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

function mockFetch(handler: FetchHandler) {
  const fn = vi.fn(handler);
  globalThis.fetch = fn as unknown as typeof fetch;
  return fn;
}

function makeToken(payload: object): string {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(JSON.stringify(payload));
  return `${header}.${body}.sig`;
}

describe("api client", () => {
  beforeEach(() => {
    localStorage.clear();
  });
  afterEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it("prefixes /api/v1 for non-/api/ paths", async () => {
    const calls: string[] = [];
    mockFetch((url) => {
      calls.push(urlToString(url));
      return new Response(JSON.stringify({ ok: true }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });
    const result = await api<{ ok: boolean }>("GET", "/rule");
    expect(calls[0]).toBe("/api/v1/rule");
    expect(result.ok).toBe(true);
  });

  it("leaves /api/ paths verbatim", async () => {
    const calls: string[] = [];
    mockFetch((url) => {
      calls.push(urlToString(url));
      return new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } });
    });
    await api("GET", "/api/v1/healthz");
    expect(calls[0]).toBe("/api/v1/healthz");
  });

  it("appends query parameters", async () => {
    const calls: string[] = [];
    mockFetch((url) => {
      calls.push(urlToString(url));
      return new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } });
    });
    await api("GET", "/rule", { query: { limit: 10, q: "host:srv-1" } });
    expect(calls[0]).toContain("limit=10");
    expect(calls[0]).toContain("q=host%3Asrv-1");
  });

  it("skips undefined query values", async () => {
    const calls: string[] = [];
    mockFetch((url) => {
      calls.push(urlToString(url));
      return new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } });
    });
    await api("GET", "/rule", { query: { limit: 10, after: undefined } });
    expect(calls[0]).toContain("limit=10");
    expect(calls[0]).not.toContain("after=");
  });

  it("adds Authorization: Bearer when a token is stored", async () => {
    writeToken(makeToken({ sub: "alice", exp: 9999999999 }));
    const headers: Record<string, string>[] = [];
    mockFetch((_url, init) => {
      const h = init?.headers as Record<string, string> | undefined;
      if (h) headers.push(h);
      return new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } });
    });
    await api("GET", "/rule");
    expect(headers[0]?.Authorization).toMatch(/^Bearer /);
  });

  it("omits Authorization when no token is stored", async () => {
    const headers: Record<string, string>[] = [];
    mockFetch((_url, init) => {
      const h = init?.headers as Record<string, string> | undefined;
      if (h) headers.push(h);
      return new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } });
    });
    await api("GET", "/rule");
    expect(headers[0]?.Authorization).toBeUndefined();
  });

  it("returns undefined on 204", async () => {
    mockFetch(() => new Response(null, { status: 204 }));
    const r = await api("DELETE", "/rule/abc");
    expect(r).toBeUndefined();
  });

  it("throws ApiError on 401", async () => {
    mockFetch(() => new Response("", { status: 401 }));
    await expect(api("GET", "/rule")).rejects.toBeInstanceOf(ApiError);
    try {
      await api("GET", "/rule");
    } catch (e) {
      expect((e as ApiError).status).toBe(401);
      expect((e as ApiError).code).toBe("unauthorized");
    }
  });

  it("parses the server's error envelope on 4xx", async () => {
    mockFetch(
      () =>
        new Response(
          JSON.stringify({
            code: "validation_error",
            detail: "name is required",
            trace_id: "abc-123",
          }),
          { status: 400, headers: { "Content-Type": "application/json" } },
        ),
    );
    try {
      await api("POST", "/rule", { body: { name: "" } });
      throw new Error("should have thrown");
    } catch (e) {
      const err = e as ApiError;
      expect(err.status).toBe(400);
      expect(err.code).toBe("validation_error");
      expect(err.detail).toBe("name is required");
      expect(err.traceId).toBe("abc-123");
    }
  });

  it("falls back to status text on non-JSON 5xx", async () => {
    mockFetch(
      () => new Response("upstream timeout", { status: 504, statusText: "Gateway Timeout" }),
    );
    try {
      await api("GET", "/rule");
      throw new Error("should have thrown");
    } catch (e) {
      const err = e as ApiError;
      expect(err.status).toBe(504);
      expect(err.code).toBe("http_504");
    }
  });

  it("sends the body as JSON for POST/PUT/PATCH", async () => {
    const bodies: string[] = [];
    mockFetch((_url, init) => {
      bodies.push(init?.body as string);
      return new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } });
    });
    await api("POST", "/rule", { body: { name: "x" } });
    expect(JSON.parse(bodies[0]!)).toEqual({ name: "x" });
  });
});

describe("api client 401 envelope", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("parses 401 envelope detail when present", async () => {
    mockFetch(
      () =>
        new Response(
          JSON.stringify({
            code: "invalid_credentials",
            detail: "Bad username or password",
          }),
          { status: 401, headers: { "Content-Type": "application/json" } },
        ),
    );
    try {
      await api("POST", "/login/local", { body: { username: "x", password: "y" } });
      throw new Error("should have thrown");
    } catch (e) {
      const err = e as ApiError;
      expect(err.status).toBe(401);
      expect(err.code).toBe("invalid_credentials");
      expect(err.detail).toBe("Bad username or password");
    }
  });

  it("skipAuthHandling avoids calling unauthorized handler on 401", async () => {
    const handler = vi.fn();
    setUnauthorizedHandler(handler);
    mockFetch(() => new Response("", { status: 401 }));
    try {
      await api("POST", "/login/local", { body: {}, skipAuthHandling: true });
    } catch {
      // expected to throw
    }
    expect(handler).not.toHaveBeenCalled();
    setUnauthorizedHandler(null);
  });
});
