import { http, HttpResponse } from "msw";

function makeStubToken(sub: string): string {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(
    JSON.stringify({
      sub,
      exp: Math.floor(Date.now() / 1000) + 3600,
      iat: Math.floor(Date.now() / 1000),
      permissions: ["ro_record", "rw_record"],
    }),
  );
  return `${header}.${body}.sig`;
}

function stubLoginEnvelope(sub: string, method: string) {
  const now = Math.floor(Date.now() / 1000);
  return {
    token: makeStubToken(sub),
    expires_at: new Date((now + 3600) * 1000).toISOString(),
    refresh_token: `refresh-${sub}-${now}`,
    refresh_expires_at: new Date((now + 7 * 24 * 3600) * 1000).toISOString(),
    method,
  };
}

export const handlers = [
  http.get("/api/v1/healthz", () => HttpResponse.json({ status: "ok" })),

  // Login backend index — tests assume all three are enabled. Individual
  // tests override via mswServer.use() when they need a narrower set.
  http.get("/api/v1/login", () =>
    HttpResponse.json({ data: { backends: ["local", "ldap", "anonymous"] } }),
  ),

  http.post("/api/v1/login/local", async ({ request }) => {
    const body = (await request.json()) as { username?: string; password?: string };
    return HttpResponse.json(stubLoginEnvelope(body.username ?? "user", "local"));
  }),

  http.post("/api/v1/login/ldap", async ({ request }) => {
    const body = (await request.json()) as { username?: string; password?: string };
    return HttpResponse.json(stubLoginEnvelope(body.username ?? "ldap-user", "ldap"));
  }),

  http.post("/api/v1/login/anonymous", () =>
    HttpResponse.json(stubLoginEnvelope("anonymous", "anonymous")),
  ),

  http.post("/api/v1/login/refresh", async ({ request }) => {
    const body = (await request.json()) as { refresh_token?: string };
    if (!body.refresh_token) {
      return new HttpResponse(null, { status: 401 });
    }
    return HttpResponse.json(stubLoginEnvelope("alice", "local"));
  }),

  http.post("/api/v1/login/logout", () => new HttpResponse(null, { status: 204 })),

  http.get("/api/v1/tenant", () =>
    HttpResponse.json({
      data: [],
      meta: { count: 0, limit: 20, offset: 0, total: 0 },
    }),
  ),

  // Catch-all list endpoint for resource-factory smoke tests.
  http.get("/api/v1/:plugin", ({ params }) => {
    return HttpResponse.json({
      data: [],
      meta: { count: 0, limit: 20, offset: 0, total: 0 },
      plugin: params.plugin,
    });
  }),
];
