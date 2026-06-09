import { describe, expect, it } from "vitest";
import { http, HttpResponse } from "msw";
import { mswServer } from "@/tests/msw/server";
import {
  fetchLoginConfig,
  loginAnonymous,
  loginLdap,
  loginLocal,
  postLogout,
  postRefresh,
  resolveTenantByKey,
} from "./api";

function makeStubToken(sub: string): string {
  const header = btoa(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const body = btoa(
    JSON.stringify({ sub, exp: Math.floor(Date.now() / 1000) + 3600, tenant_id: "acme" }),
  );
  return `${header}.${body}.sig`;
}

describe("login API", () => {
  it("loginLocal returns a token + refresh token from MSW", async () => {
    const result = await loginLocal({ username: "alice", password: "secret" });
    expect(typeof result.token).toBe("string");
    expect(result.token.split(".").length).toBe(3);
    expect(result.refreshToken).toMatch(/^refresh-alice-/);
  });

  it("loginLdap returns a token + refresh token from MSW", async () => {
    const result = await loginLdap({ username: "alice", password: "secret" });
    expect(result.token.split(".").length).toBe(3);
    expect(result.refreshToken).toMatch(/^refresh-alice-/);
  });

  it("loginAnonymous returns a token + refresh token from MSW", async () => {
    const result = await loginAnonymous();
    expect(result.token.split(".").length).toBe(3);
    expect(result.refreshToken).toMatch(/^refresh-anonymous-/);
  });

  it("postRefresh exchanges a refresh token for a new pair", async () => {
    const refreshed = await postRefresh("seed-refresh");
    expect(refreshed.token.split(".").length).toBe(3);
    expect(refreshed.refreshToken).toMatch(/^refresh-alice-/);
  });

  it("postLogout never throws", async () => {
    await expect(postLogout("seed-refresh")).resolves.toBeUndefined();
    await expect(postLogout(null)).resolves.toBeUndefined();
  });
});

describe("loginLocal with org", () => {
  it("sends org in the request body when provided", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/login/local", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({
          token: makeStubToken("alice"),
          expires_at: new Date(Date.now() + 3600000).toISOString(),
          method: "local",
        });
      }),
    );
    await loginLocal({ username: "alice", password: "pw", org: "acme" });
    expect((bodies[0] as { org?: string }).org).toBe("acme");
  });

  it("omits org when not provided", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/login/local", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({
          token: makeStubToken("alice"),
          expires_at: new Date(Date.now() + 3600000).toISOString(),
          method: "local",
        });
      }),
    );
    await loginLocal({ username: "alice", password: "pw" });
    expect((bodies[0] as { org?: string }).org).toBeUndefined();
  });
});

describe("loginLdap with org", () => {
  it("sends org in the request body when provided", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/login/ldap", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({
          token: makeStubToken("ldap-user"),
          expires_at: new Date(Date.now() + 3600000).toISOString(),
          method: "ldap",
        });
      }),
    );
    await loginLdap({ username: "ldap-user", password: "pw", org: "acme" });
    expect((bodies[0] as { org?: string }).org).toBe("acme");
  });
});

describe("auth api tenant discovery", () => {
  it("fetchLoginConfig returns backends + tenants", async () => {
    mswServer.use(
      http.get("/api/v1/login", () =>
        HttpResponse.json({
          data: { backends: ["local", "ldap"], tenants: [{ id: "acme", display_name: "Acme" }] },
        }),
      ),
    );
    const cfg = await fetchLoginConfig();
    expect(cfg.backends).toEqual(["local", "ldap"]);
    expect(cfg.tenants).toEqual([{ id: "acme", display_name: "Acme" }]);
  });

  it("fetchLoginConfig returns empty tenants when server omits the field", async () => {
    // default MSW handler returns no tenants field — fetchLoginConfig must still work
    const cfg = await fetchLoginConfig();
    expect(cfg.backends).toEqual(["local", "ldap", "anonymous"]);
    expect(cfg.tenants).toEqual([]);
  });

  it("resolveTenantByKey returns the tenant on 200", async () => {
    mswServer.use(
      http.get("/api/v1/login/tenant", () =>
        HttpResponse.json({ data: { id: "acme", display_name: "Acme" } }),
      ),
    );
    expect(await resolveTenantByKey("KEY")).toEqual({ id: "acme", display_name: "Acme" });
  });

  it("resolveTenantByKey returns null on error (no leak)", async () => {
    mswServer.use(http.get("/api/v1/login/tenant", () => new HttpResponse(null, { status: 404 })));
    expect(await resolveTenantByKey("BAD")).toBeNull();
  });
});
