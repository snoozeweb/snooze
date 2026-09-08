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

export type LoginSession = { token: string; refreshToken: string | null };

export type SnoozeApi = {
  baseURL: string;
  token: string;
  ctx: APIRequestContext;
  loginLocal(username: string, password: string): Promise<string>;
  /** Full login envelope — the access token plus the refresh token the SPA
   *  uses to renew it. Needed by tests that exercise session expiry. */
  loginSession(username: string, password: string): Promise<LoginSession>;
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
  /** Delivery history rows — the `notificationlog` collection (Deliveries). */
  notificationlog: ResourceApi;
  /** Raw delivery-log rows, newest first. */
  listDeliveries(): Promise<DeliveryLogRow[]>;
  /**
   * Polls the delivery log until `want` is satisfied, then returns the rows
   * (newest first).
   *
   * Delivery rows are written OUT OF BAND: `notification.Process` spawns a
   * detached coordinator goroutine that sends, resolves the record uid and
   * only then writes the row — a few hundred ms after the ingest POST has
   * already returned 200. Batched actions defer even further (the row lands
   * at flush). So every assertion about a delivery has to wait for it, and
   * every wait has to be a poll rather than a sleep.
   *
   * `want` is either a row count (">= n rows exist") or a predicate over the
   * whole list, which is what tests use when they need a *specific* row
   * ("the one for act-fail") rather than just any n.
   */
  waitForDeliveries(
    want: number | ((rows: DeliveryLogRow[]) => boolean),
    opts?: { timeout?: number },
  ): Promise<DeliveryLogRow[]>;
  /**
   * Blocks until the notification dispatcher is demonstrably live, then
   * leaves the alerts table and the delivery log empty.
   *
   * The notification plugin serves matches from a cache reloaded off the
   * syncer bus, so a notification is NOT active the instant its POST returns
   * — and an alert injected into that window is silently never dispatched,
   * which surfaces as a delivery row that never appears. Waiting on the
   * *route* is not enough either: the only observable proof that the cache
   * swapped is a send that actually happened.
   *
   * So: a throwaway route matching only `source: "gate"` is created, and gate
   * alerts (one distinct host per attempt — see the note at the send) are
   * injected until one of them produces a delivery row. A reload swaps the
   * whole entry set at once — once the gate route is live, every route
   * created BEFORE it is live too. Call this AFTER seeding the routes under
   * test and BEFORE injecting the alert those routes should deliver.
   */
  warmUpDispatcher(opts?: { timeout?: number }): Promise<void>;
};

/**
 * One delivery-log row, as the server stores it. Only the fields the e2e
 * specs assert on are named — the row carries more (see the
 * `NotificationLogEntry` schema in api/openapi.yaml).
 */
export type DeliveryLogRow = {
  uid?: string;
  date_epoch?: number;
  status?: "success" | "error";
  error?: string;
  action?: string;
  notifier?: string;
  batch?: boolean;
  batch_reason?: string;
  notification_uids?: string[];
  notification_names?: string[];
  alert_count?: number;
  alert_uids?: string[];
  alert_hashes?: string[];
  alerts?: { uid?: string; hash?: string; host?: string; severity?: string; message?: string }[];
};

/** Default budget for waitForDeliveries. Generous on purpose: under
 *  `workers=2` the send, the record-uid resolution and the row write all
 *  compete with the other spec files' traffic on the same server. */
const DELIVERY_WAIT_TIMEOUT = 20_000;
const DELIVERY_POLL_INTERVAL = 200;
/** Grace period for in-flight coordinator goroutines before a wipe. The write
 *  itself takes single-digit ms; this is slack, not a measured latency. */
const DELIVERY_SETTLE_MS = 750;

export type ResourceApi = {
  create(body: Record<string, unknown>): Promise<{ uid: string }>;
  list(): Promise<{ uid: string }[]>;
  remove(uid: string): Promise<void>;
  clear(): Promise<void>;
};

function resourceApi(
  ctx: APIRequestContext,
  baseURL: string,
  plugin: string,
  token: string,
): ResourceApi {
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
    async loginSession(username, password) {
      const r = await ctx.post(`${baseURL}/api/v1/login/local`, { data: { username, password } });
      if (!r.ok()) throw new Error(`login: ${r.status()} ${await r.text()}`);
      const out = (await r.json()) as { token: string; refresh_token?: string };
      return { token: out.token, refreshToken: out.refresh_token ?? null };
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
        self.notificationlog.clear(),
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
        // Sequential delete loop with per-response checks. Firing every remove
        // with Promise.all raced the server's audit + cleanup writers under
        // load and returned occasional 5xx — and because the old code never
        // inspected the responses, a failed delete was silently swallowed and
        // the row survived into the *next* test's seed, skewing its counts
        // (e.g. select-all reporting "7 selected" instead of "5", or a leftover
        // acked row hiding from the open Alerts tab). The generic
        // resourceApi.clear() above was already fixed the same way; keep this
        // alerts-specific loop in lockstep.
        const items = (await this.list()) as { uid?: string }[];
        for (const a of items) {
          if (!a.uid) continue;
          const r = await ctx.delete(`${baseURL}/api/v1/record/${a.uid}`, { headers });
          if (!r.ok() && r.status() !== 404) {
            throw new Error(`clear record ${a.uid}: ${r.status()} ${await r.text()}`);
          }
        }
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
    // Delivery history. Written by the notification dispatcher and the
    // batching notifiers, never by a test — but `clear()` matters: rows are
    // cumulative and outlive alerts.clear(), so a spec that counts them has
    // to start from zero. DELETE needs rw_notificationlog; the harness token
    // is the root token (rw_all), so the generic per-uid loop is enough.
    notificationlog: resourceApi(ctx, baseURL, "notificationlog", token),

    async listDeliveries() {
      const r = await ctx.get(
        `${baseURL}/api/v1/notificationlog?limit=500&orderby=date_epoch&asc=false`,
        { headers },
      );
      if (!r.ok()) throw new Error(`list notificationlog: ${r.status()} ${await r.text()}`);
      const out = (await r.json()) as { data?: DeliveryLogRow[] };
      return out.data ?? [];
    },

    async waitForDeliveries(want, opts) {
      const self = this as SnoozeApi;
      const deadline = Date.now() + (opts?.timeout ?? DELIVERY_WAIT_TIMEOUT);
      const done =
        typeof want === "number" ? (rows: DeliveryLogRow[]) => rows.length >= want : want;
      let rows: DeliveryLogRow[] = [];
      for (;;) {
        rows = await self.listDeliveries();
        if (done(rows)) return rows;
        if (Date.now() >= deadline) {
          throw new Error(
            `waitForDeliveries timed out (${typeof want === "number" ? `wanted ${want} rows` : "predicate"}); ` +
              `saw ${rows.length}: ${JSON.stringify(
                rows.map((r) => ({
                  action: r.action,
                  status: r.status,
                  batch: r.batch,
                  alerts: r.alert_count,
                })),
              )}`,
          );
        }
        await new Promise((r) => setTimeout(r, DELIVERY_POLL_INTERVAL));
      }
    },

    async warmUpDispatcher(opts) {
      const self = this as SnoozeApi;
      const stamp = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
      const gateAction = `gate-act-${stamp}`;
      const gateNotif = `gate-notif-${stamp}`;
      const action = await self.actions.create({
        name: gateAction,
        action: { selected: "script", subcontent: { command: ["/bin/true"] } },
      });
      const notification = await self.notifications.create({
        name: gateNotif,
        enabled: true,
        condition: { type: "=", field: "source", value: "gate" },
        actions: [gateAction],
      });

      const deadline = Date.now() + (opts?.timeout ?? DELIVERY_WAIT_TIMEOUT);
      const landed = () =>
        self
          .listDeliveries()
          .then((rows) => rows.some((r) => r.action === gateAction && r.status === "success"));
      for (let attempt = 0; ; attempt++) {
        // Re-injected rather than sent once: the send itself is the probe, and
        // a probe that fired too early has to be repeated, not waited on.
        //
        // Each probe carries a DISTINCT host. Re-sending the same host+message
        // would aggregate onto the first record and be throttled by the
        // default "Host and Message" aggregate rule — the retries would then
        // never reach the dispatcher and the warm-up could only ever succeed
        // on its first attempt, which is the very case it exists to cover.
        await self.alerts.send({
          host: `gate-host-${stamp}-${attempt}`,
          message: "dispatcher warm-up",
          severity: "info",
          source: "gate",
        });
        let ok = false;
        for (let i = 0; i < 5 && !ok; i++) {
          await new Promise((r) => setTimeout(r, DELIVERY_POLL_INTERVAL));
          ok = await landed();
        }
        if (ok) break;
        if (Date.now() >= deadline) {
          throw new Error("warmUpDispatcher: the notification cache never went live");
        }
      }

      // Retire the gate route FIRST so no further gate alert can be
      // dispatched, then let the in-flight coordinator goroutines finish
      // before wiping — a row written after the clear would land in the
      // caller's "exactly N rows" assertion.
      await self.notifications.remove(notification.uid);
      await self.actions.remove(action.uid);
      await new Promise((r) => setTimeout(r, DELIVERY_SETTLE_MS));
      await self.alerts.clear();
      await self.notificationlog.clear();
    },
  };
}
