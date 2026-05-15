// web/tests/e2e/tour.spec.ts
//
// Visual tour: seeds varied data through the API, then walks every top-level
// route + a few drawers/dialogs taking a screenshot of each. Skipped by
// default; run with `SNOOZE_TOUR=1 npx playwright test tests/e2e/tour.spec.ts`.
//
// Screenshots are written to tests/e2e/.screenshots/ (gitignored).
import { dirname, resolve } from "node:path";
import { mkdirSync, rmSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { test } from "./harness/fixtures";

const __dirname = dirname(fileURLToPath(import.meta.url));
const SHOTS_DIR = resolve(__dirname, ".screenshots");

test.describe.configure({ mode: "serial" });
test.skip(!process.env.SNOOZE_TOUR, "set SNOOZE_TOUR=1 to run the visual tour");

test("visual tour: seed and screenshot every menu", async ({
  page,
  api,
  server,
  adminAuth,
}) => {
  test.setTimeout(180_000);
  rmSync(SHOTS_DIR, { recursive: true, force: true });
  mkdirSync(SHOTS_DIR, { recursive: true });

  const shoot = async (name: string) => {
    await page.screenshot({ path: resolve(SHOTS_DIR, `${name}.png`), fullPage: false });
  };
  const goto = async (route: string) => {
    await page.goto(server.baseURL + route);
    await page.waitForLoadState("networkidle").catch(() => {});
  };

  // ── 1. Auth + seed ───────────────────────────────────────────────────────
  await adminAuth();

  await api.environments.create({
    name: "production",
    color: "#3fb950",
    comment: "Customer-facing servers",
  });
  await api.environments.create({
    name: "staging",
    color: "#d4a017",
    comment: "Pre-prod mirror",
  });
  await api.environments.create({
    name: "dev",
    color: "#4f8cff",
  });

  await api.rules.create({
    name: "tag-production",
    enabled: true,
    condition: { type: "MATCHES", field: "host", value: "^srv-prod-" },
    modifications: [["SET", "environment", "production"]],
    comment: "Mark prod-host alerts so dashboards filter cleanly",
  });
  await api.rules.create({
    name: "tag-staging",
    enabled: false,
    condition: { type: "MATCHES", field: "host", value: "^srv-stage-" },
    modifications: [["SET", "environment", "staging"]],
    comment: "Currently disabled — too noisy",
  });
  await api.rules.create({
    name: "drop-heartbeat",
    enabled: true,
    condition: { type: "EQUALS", field: "message", value: "Heartbeat ok" },
    modifications: [],
    comment: "Reserved: will gain a DROP action later",
  });

  await api.snoozes.create({
    name: "weekend-window",
    enabled: true,
    condition: { type: "EQUALS", field: "host", value: "noisy-1" },
    ttl: 60 * 60 * 48,
    comment: "Mute weekend cron flapping for 48h",
  });
  await api.snoozes.create({
    name: "edge-eu-maintenance",
    enabled: false,
    condition: { type: "EQUALS", field: "host", value: "edge-eu" },
    ttl: 3600,
  });

  await api.actions.create({
    name: "slack-alerts",
    action_type: "script",
    action: { command: "/usr/local/bin/post-slack" },
    comment: "Posts to #alerts via webhook",
  });
  await api.actions.create({
    name: "pagerduty",
    action_type: "script",
    action: { command: "/usr/local/bin/pd-trigger" },
  });
  await api.notifications.create({
    name: "critical-page-oncall",
    enabled: true,
    condition: { type: "EQUALS", field: "severity", value: "critical" },
    actions: ["pagerduty", "slack-alerts"],
    comment: "Page oncall + post to Slack for criticals",
  });
  await api.notifications.create({
    name: "warning-slack-only",
    enabled: true,
    condition: { type: "EQUALS", field: "severity", value: "warning" },
    actions: ["slack-alerts"],
  });

  await api.users.create({
    name: "alice",
    type: "local",
    roles: ["admin"],
    comment: "On-call lead",
  });
  await api.users.create({
    name: "bob",
    type: "local",
    roles: ["analyst"],
  });
  await api.users.create({
    name: "carol",
    type: "ldap",
    roles: ["viewer"],
    comment: "External auditor",
  });

  await api.roles.create({
    name: "analyst",
    permissions: ["ro_rule", "ro_snooze", "rw_record", "ro_notification"],
    comment: "Triage role: read configs, manage alerts",
  });
  // bootstrap already seeds admin/viewer/notifications; create a distinct
  // role so we don't collide with the now-enforced primary-key.
  await api.roles.create({
    name: "oncall",
    permissions: ["ro_rule", "rw_snooze", "rw_record", "ro_notification"],
    comment: "Read configs, manage snoozes/alerts during a shift",
  });

  await api.widgets.create({
    name: "patlite-floor1",
    widget_type: "patlite",
    config: { addr: "10.0.0.10", port: 80 },
    comment: "First-floor tower light",
  });
  await api.widgets.create({
    name: "grafana-embed",
    widget_type: "iframe",
    config: { src: "https://grafana.example/d/abc/overview" },
  });

  await api.kv.create({
    key: "DEFAULT_RETENTION_DAYS",
    value: "30",
    comment: "Used by housekeeper to expire records",
  });
  await api.kv.create({
    key: "ON_CALL_TEAM",
    value: "infra-pager",
  });

  // The syncer debounces collection-change events (~100ms) before calling
  // Reload on the rule plugin. Wait it out before ingesting alerts so the
  // tag-production rule is in the cache when the prod-host alerts arrive.
  await new Promise((r) => setTimeout(r, 500));

  // 10 alerts spanning severities/sources/states.
  await api.alerts.sendMany([
    {
      host: "srv-prod-1",
      message: "Disk usage above 90%",
      severity: "critical",
      source: "prometheus",
    },
    {
      host: "srv-prod-2",
      message: "OOM kill: nginx",
      severity: "error",
      source: "syslog",
    },
    {
      host: "srv-prod-3",
      message: "CPU > 95% for 5m",
      severity: "warning",
      source: "prometheus",
    },
    {
      host: "srv-stage-1",
      message: "Deploy started — release/2026.05",
      severity: "info",
      source: "ci",
    },
    {
      host: "srv-stage-2",
      message: "Slow query > 500ms (lookup_user)",
      severity: "warning",
      source: "postgres",
    },
    {
      host: "noisy-1",
      message: "Heartbeat ok",
      severity: "info",
      source: "ping",
    },
    {
      host: "db-master",
      message: "Replication lag 12s",
      severity: "warning",
      source: "pg-replication",
    },
    {
      host: "edge-eu",
      message: "SSL cert expires in 7 days",
      severity: "warning",
      source: "ssl-monitor",
    },
    {
      host: "edge-us",
      message: "503 rate spike (1.2%/min)",
      severity: "error",
      source: "envoy",
    },
    {
      host: "auth",
      message: "OAuth token revoked by admin",
      severity: "info",
      source: "audit",
    },
  ]);

  // Settle: rule Reload is debounced, alerts ingest is async.
  await new Promise((r) => setTimeout(r, 800));

  // ── 2. Top-level pages ───────────────────────────────────────────────────
  await goto("/web/alerts");
  await shoot("01-alerts");

  await goto("/web/dashboard");
  await shoot("02-dashboard");

  await goto("/web/snoozes");
  await shoot("03-snoozes");

  await goto("/web/rules");
  await shoot("04-rules");

  await page.getByRole("tab", { name: /aggregates/i }).click({ force: true });
  await shoot("05-rules-aggregates");

  await goto("/web/notifications");
  await shoot("06-notifications");

  await page.getByRole("tab", { name: /^actions$/i }).click({ force: true });
  await shoot("07-notifications-actions");

  await goto("/web/admin/users");
  await shoot("08-admin-users");

  await goto("/web/admin/roles");
  await shoot("09-admin-roles");

  await goto("/web/admin/environments");
  await shoot("10-admin-environments");

  await goto("/web/admin/widgets");
  await shoot("11-admin-widgets");

  await goto("/web/admin/kv");
  await shoot("12-admin-kv");

  await goto("/web/admin/settings");
  await shoot("13-admin-settings");

  await goto("/web/admin/status");
  await shoot("14-admin-status");

  // ── 3. Drawers / dialogs ─────────────────────────────────────────────────
  await goto("/web/rules");
  await page.getByRole("button", { name: /^new$/i }).click({ force: true });
  await shoot("20-new-rule-drawer");
  await page.keyboard.press("Escape");

  await page.getByText("tag-production").click({ force: true });
  await shoot("21-edit-rule-drawer");
  await page.keyboard.press("Escape");

  await goto("/web/snoozes");
  await page.getByRole("button", { name: /^new$/i }).click({ force: true });
  await shoot("22-new-snooze-drawer");
  await page.keyboard.press("Escape");

  await goto("/web/notifications");
  await page.getByText("critical-page-oncall").click({ force: true });
  await shoot("23-edit-notification-drawer");
  await page.keyboard.press("Escape");

  await goto("/web/alerts");
  await page.getByText("srv-prod-1").first().click({ force: true });
  await shoot("24-alert-detail-drawer");
  await page.keyboard.press("Escape");

  // Row-actions menu: bulk-ack dialog
  await page.locator("thead").getByRole("checkbox").first().check({ force: true });
  await page.getByRole("button", { name: /acknowledge \(\d+\)/i }).click({ force: true });
  await shoot("25-bulk-ack-dialog");
  await page.keyboard.press("Escape");

  // Command palette — focus by tabbing past the topbar so we don't click a
  // table row (which would otherwise open the detail drawer underneath the
  // palette). The keyboard listener is on `window`, so any element having
  // focus works.
  await goto("/web/alerts");
  await page.keyboard.press("Tab");
  await page.keyboard.press("Control+K");
  await shoot("26-command-palette");
  await page.keyboard.press("Escape");

  // ── 4. Light theme variant of a couple of pages ─────────────────────────
  await goto("/web/alerts");
  await page
    .getByRole("button", { name: /switch to (light|dark) theme/i })
    .click({ force: true });
  await shoot("30-alerts-light");
  await goto("/web/dashboard");
  await shoot("31-dashboard-light");
});
