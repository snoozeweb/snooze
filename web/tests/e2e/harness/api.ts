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
  comments: ResourceApi;
  settings: ResourceApi;
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
      // The CRUD POST handler (internal/plugins/crud.go createHandler) responds
      // with a JSON-encoded db.WriteResult: {Added,Updated,Replaced,Rejected}.
      // Older harness code expected {data:{uid}} and silently returned "" for
      // every uid — fine for tests that never read it back, but the tour
      // needs the uid to wire a parent/child rule tree. Tolerate both shapes.
      const out = (await r.json()) as {
        Added?: string[];
        data?: { uid?: string } | { uid?: string }[];
      };
      const fromWriteResult = out.Added?.[0];
      const fromData = Array.isArray(out.data) ? out.data[0]?.uid : out.data?.uid;
      return { uid: fromWriteResult ?? fromData ?? "" };
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
      // Sequential delete loop. We used to fire all removes with
      // Promise.all, which raced the server's audit pipeline + cleanup
      // writers under load and surfaced as occasional 5xx on the second-
      // to-last delete (the test would then fail with "delete X: 500" in
      // beforeEach when the suite ran with multiple workers). One round
      // trip per item is slower but deterministic, and clear() is only
      // called in beforeEach so the wall-time impact is negligible.
      const items = await this.list();
      for (const i of items) {
        await this.remove(i.uid);
      }
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
    // Alerts comments / acks. The Comment plugin (internal/pluginimpl/comment)
    // stores entries with shape {record_uid, type, message, date_epoch, user}.
    // The web UI's AlertDetailDrawer renders these in the timeline pane.
    comments: resourceApi(ctx, baseURL, "comment", token),
    // Runtime settings. The settings plugin (internal/pluginimpl/settings)
    // tolerates arbitrary documents at the CRUD layer — its Validate only
    // rejects an empty `section` field if supplied. The web UI's
    // SettingsPage treats each row as a flat KV doc with {name, value,
    // comment}; seeding with that shape matches what SettingEditor writes.
    settings: resourceApi(ctx, baseURL, "settings", token),
  };
}
