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

export const handlers = [
  http.get("/api/v1/healthz", () => HttpResponse.json({ status: "ok" })),

  http.post("/api/v1/login/local", async ({ request }) => {
    const body = (await request.json()) as { username?: string; password?: string };
    return HttpResponse.json({ token: makeStubToken(body.username ?? "user") });
  }),

  http.post("/api/v1/login/ldap", async ({ request }) => {
    const body = (await request.json()) as { username?: string; password?: string };
    return HttpResponse.json({ token: makeStubToken(body.username ?? "ldap-user") });
  }),

  http.post("/api/v1/login/anonymous", () =>
    HttpResponse.json({ token: makeStubToken("anonymous") }),
  ),

  // Catch-all list endpoint for resource-factory smoke tests.
  http.get("/api/v1/:plugin", ({ params }) => {
    return HttpResponse.json({ items: [], total: 0, plugin: params.plugin });
  }),
];
