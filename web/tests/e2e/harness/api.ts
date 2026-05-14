// web/tests/e2e/harness/api.ts
import { request as pwRequest, type APIRequestContext } from "@playwright/test";

export type RootTokenSource = { adminSocketPath: string };

export async function mintRootToken(src: RootTokenSource): Promise<string> {
  // The admin socket is a Unix socket serving HTTP. We call it via fetch with a
  // custom dispatcher would be cleanest, but node's `undici` requires a small
  // helper. For E2E we shell out to the snooze-server binary's `root-token`
  // subcommand — already tested and supported.
  const { execFileSync } = await import("node:child_process");
  const { binPath } = await import("./paths");
  const out = execFileSync(binPath, ["root-token", "--socket", src.adminSocketPath], {
    encoding: "utf-8",
  });
  const body = out.trim();
  // The admin socket returns {"root_token":"...","expires_at":"..."}.
  // Tolerate bare token strings as a fallback.
  try {
    const json = JSON.parse(body) as { root_token?: string; token?: string };
    if (json.root_token) return json.root_token;
    if (json.token) return json.token;
  } catch {
    /* fall through */
  }
  return body;
}

export type SnoozeApi = {
  baseURL: string;
  token: string;
  ctx: APIRequestContext;
  loginLocal(username: string, password: string): Promise<string>;
  reset(): Promise<void>;
  alerts: {
    send(record: Record<string, unknown>): Promise<void>;
    sendMany(records: Record<string, unknown>[]): Promise<void>;
    list(): Promise<unknown[]>;
    clear(): Promise<void>;
  };
  rules: ResourceApi;
  aggregaterules: ResourceApi;
  snoozes: ResourceApi;
  notifications: ResourceApi;
  actions: ResourceApi;
  users: ResourceApi;
  roles: ResourceApi;
  environments: ResourceApi;
  widgets: ResourceApi;
  kv: ResourceApi;
};

export type ResourceApi = {
  create(body: Record<string, unknown>): Promise<{ uid: string }>;
  list(): Promise<{ uid: string }[]>;
  remove(uid: string): Promise<void>;
  clear(): Promise<void>;
};

function resourceApi(ctx: APIRequestContext, baseURL: string, plugin: string, token: string): ResourceApi {
  const headers = { Authorization: `Bearer ${token}` };
  return {
    async create(body) {
      const r = await ctx.post(`${baseURL}/api/v1/${plugin}`, { headers, data: body });
      if (!r.ok()) throw new Error(`create ${plugin}: ${r.status()} ${await r.text()}`);
      const out = (await r.json()) as { data?: { uid: string } };
      return { uid: out.data?.uid ?? "" };
    },
    async list() {
      const r = await ctx.get(`${baseURL}/api/v1/${plugin}?limit=500`, { headers });
      if (!r.ok()) throw new Error(`list ${plugin}: ${r.status()}`);
      const out = (await r.json()) as { data?: { uid: string }[] };
      return out.data ?? [];
    },
    async remove(uid) {
      const r = await ctx.delete(`${baseURL}/api/v1/${plugin}/${uid}`, { headers });
      if (!r.ok() && r.status() !== 404) throw new Error(`delete ${plugin}/${uid}: ${r.status()}`);
    },
    async clear() {
      const items = await this.list();
      await Promise.all(items.map((i) => this.remove(i.uid)));
    },
  };
}

export async function createApi(baseURL: string, token: string): Promise<SnoozeApi> {
  const ctx = await pwRequest.newContext();
  const headers = { Authorization: `Bearer ${token}` };

  return {
    baseURL,
    token,
    ctx,
    async loginLocal(username, password) {
      const r = await ctx.post(`${baseURL}/api/v1/login/local`, { data: { username, password } });
      if (!r.ok()) throw new Error(`login: ${r.status()} ${await r.text()}`);
      const out = (await r.json()) as { token: string };
      return out.token;
    },
    async reset() {
      // `this` is typed as the inferred object literal — cast to the public
      // interface so TypeScript accepts member accesses below.
      const self = this as SnoozeApi;
      await Promise.all([
        self.rules.clear(),
        self.aggregaterules.clear(),
        self.snoozes.clear(),
        self.notifications.clear(),
        self.actions.clear(),
        self.environments.clear(),
        self.widgets.clear(),
        self.kv.clear(),
        self.alerts.clear(),
      ]);
    },
    alerts: {
      async send(record) {
        const r = await ctx.post(`${baseURL}/api/v1/alerts`, { headers, data: record });
        if (!r.ok()) throw new Error(`send alert: ${r.status()} ${await r.text()}`);
      },
      async sendMany(records) {
        const r = await ctx.post(`${baseURL}/api/v1/alerts`, { headers, data: records });
        if (!r.ok()) throw new Error(`send alerts: ${r.status()} ${await r.text()}`);
      },
      async list() {
        // Alerts are stored in the `record` plugin collection.
        // Verified against api/openapi.yaml PluginPath enum.
        const r = await ctx.get(`${baseURL}/api/v1/record?limit=500`, { headers });
        if (!r.ok()) throw new Error(`list alerts: ${r.status()}`);
        const out = (await r.json()) as { data?: unknown[] };
        return out.data ?? [];
      },
      async clear() {
        const items = (await this.list()) as { uid?: string }[];
        await Promise.all(
          items
            .filter((a) => a.uid)
            .map((a) =>
              ctx.delete(`${baseURL}/api/v1/record/${a.uid}`, { headers }),
            ),
        );
      },
    },
    // Plugin names verified against api/openapi.yaml PluginPath enum (line 566–590).
    rules: resourceApi(ctx, baseURL, "rule", token),
    aggregaterules: resourceApi(ctx, baseURL, "aggregaterule", token),
    snoozes: resourceApi(ctx, baseURL, "snooze", token),
    notifications: resourceApi(ctx, baseURL, "notification", token),
    actions: resourceApi(ctx, baseURL, "action", token),
    users: resourceApi(ctx, baseURL, "user", token),
    roles: resourceApi(ctx, baseURL, "role", token),
    environments: resourceApi(ctx, baseURL, "environment", token),
    widgets: resourceApi(ctx, baseURL, "widget", token),
    kv: resourceApi(ctx, baseURL, "kv", token),
  };
}
