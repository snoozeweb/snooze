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
import { expect } from "@playwright/test";
import { test } from "./harness/fixtures";

const __dirname = dirname(fileURLToPath(import.meta.url));
const SHOTS_DIR = resolve(__dirname, ".screenshots");

test.describe.configure({ mode: "serial" });
test.skip(!process.env.SNOOZE_TOUR, "set SNOOZE_TOUR=1 to run the visual tour");

test("visual tour: seed and screenshot every menu", async ({ page, api, server, adminAuth }) => {
  test.setTimeout(180_000);
  rmSync(SHOTS_DIR, { recursive: true, force: true });
  mkdirSync(SHOTS_DIR, { recursive: true });

  // Use a raw CDP session for screenshots. Playwright's `page.screenshot()`
  // adds an internal `document.fonts.ready` wait that reliably hangs on
  // WSL2 + chromium-1148 (the wait reports "fonts loaded" but the
  // subsequent CDP `Page.captureScreenshot` call never returns). Talking
  // to CDP directly skips that pre-wait and returns in milliseconds.
  const cdp = await page.context().newCDPSession(page);
  const { writeFile } = await import("node:fs/promises");
  const shoot = async (name: string) => {
    // JPEG instead of PNG — png encoding on this chromium-1148 + WSL2 combo
    // hangs the CDP `Page.captureScreenshot` call indefinitely.
    const { data } = await cdp.send("Page.captureScreenshot", {
      format: "jpeg",
      quality: 80,
      captureBeyondViewport: false,
    });
    await writeFile(resolve(SHOTS_DIR, `${name}.jpg`), Buffer.from(data, "base64"));
  };
  const goto = async (route: string) => {
    await page.goto(server.baseURL + route);
    await page.waitForLoadState("networkidle").catch(() => {});
  };

  // ── 1. Auth + seed ───────────────────────────────────────────────────────
  // Screenshot the login page first, before injecting the admin token —
  // once adminAuth() drops the JWT into localStorage, any /web/login
  // visit redirects back to /web/alerts (see web/src/app/router.tsx).
  await goto("/web/login");
  // Wait for the centered logo + the Local-tab Sign-in button so we don't
  // capture mid-mount. The tab list's Local tab is selected by default.
  await page.getByRole("img", { name: /snooze/i }).waitFor({ state: "visible" });
  await page.getByRole("button", { name: /sign in$/i }).waitFor({ state: "visible" });
  await shoot("00-login");

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

  // Nested-condition rule: deeply nested AND/OR/NOT tree, so the
  // ConditionEditor in Builder mode renders the recursive group widget.
  // This is the screen the user pointed out was missing from the tour.
  await api.rules.create({
    name: "page-prod-criticals",
    enabled: true,
    condition: {
      type: "AND",
      args: [
        {
          type: "OR",
          args: [
            { type: "MATCHES", field: "host", value: "^srv-prod-" },
            { type: "EQUALS", field: "environment", value: "production" },
          ],
        },
        { type: "NOT", arg: { type: "EXISTS", field: "shelved" } },
        {
          type: "OR",
          args: [
            { type: "EQUALS", field: "severity", value: "critical" },
            { type: "EQUALS", field: "severity", value: "error" },
          ],
        },
      ],
    },
    modifications: [["SET", "page_oncall", true]],
    comment: "Nested example: any prod host, not shelved, critical or error.",
  });

  // Parent → child → grandchild rule tree. Mirrors the Python v1 model
  // (parents[]) and the Go backend's recursive evaluator (rule/plugin.go
  // processRules). Lets the rule-list screenshot show that nesting is real
  // and that tree_order pins sibling sequence.
  const parentRule = await api.rules.create({
    name: "tree-root: tag-by-region",
    enabled: true,
    condition: { type: "MATCHES", field: "host", value: "^edge-" },
    modifications: [["SET", "region", "edge"]],
    tree_order: 10,
    comment: "Root of a 3-level rule tree; children only run if this matches.",
  });
  const childRule = await api.rules.create({
    name: "tree-child: edge-eu",
    enabled: true,
    condition: { type: "EQUALS", field: "host", value: "edge-eu" },
    modifications: [["SET", "region", "eu-west"]],
    parents: [parentRule.uid],
    tree_order: 0,
    comment: "Child of tree-root; only runs because parent matched.",
  });
  await api.rules.create({
    name: "tree-grandchild: ssl-bump",
    enabled: true,
    condition: { type: "MATCHES", field: "message", value: "SSL" },
    modifications: [["SET", "priority", "p2"]],
    parents: [childRule.uid],
    tree_order: 0,
    comment: "Grand-child; only runs if eu-west AND SSL message.",
  });

  // Aggregate rule with full field set so the Aggregates tab + edit form
  // both have real content to capture in the tour.
  await api.aggregaterules.create({
    name: "flapping-hosts",
    enabled: true,
    condition: { type: "EQUALS", field: "severity", value: "warning" },
    fields: ["host", "source"],
    watch: ["severity", "state"],
    throttle: 600,
    comment: "Collapse repeated flap notifications per host+source.",
  });

  await api.snoozes.create({
    name: "weekend-window",
    enabled: true,
    condition: { type: "EQUALS", field: "host", value: "noisy-1" },
    // Time-constraint demo: weekends only, 22:00–06:00 quiet hours.
    // No datetime constraint → snooze is "Active" forever.
    time_constraints: {
      weekdays: [{ weekdays: [0, 6] }], // Sun, Sat
      time: [{ from: "22:00", until: "06:00" }],
    },
    comment: "Mute weekend cron flapping",
  });
  await api.snoozes.create({
    name: "edge-eu-maintenance",
    enabled: false,
    condition: { type: "EQUALS", field: "host", value: "edge-eu" },
    discard: true,
    comment: "Drop edge-eu alerts entirely during the maintenance window",
  });
  // Upcoming: datetime.from in the future → lands in the Upcoming tab.
  const upcomingFrom = new Date(Date.now() + 86400 * 1000 * 7).toISOString();
  const upcomingUntil = new Date(Date.now() + 86400 * 1000 * 10).toISOString();
  await api.snoozes.create({
    name: "release-window-2026Q3",
    enabled: true,
    condition: { type: "EQUALS", field: "environment", value: "production" },
    time_constraints: {
      datetime: [{ from: upcomingFrom, until: upcomingUntil }],
    },
    comment: "Planned mute for the upcoming release rollout",
  });
  // Expired: datetime.until in the past → lands in the Expired tab.
  const expiredUntil = new Date(Date.now() - 86400 * 1000 * 3).toISOString();
  await api.snoozes.create({
    name: "old-incident-2026-INC-042",
    enabled: true,
    condition: { type: "EQUALS", field: "host", value: "auth" },
    time_constraints: {
      datetime: [{ from: "2026-05-01T00:00:00Z", until: expiredUntil }],
    },
    comment: "Closed incident — left in place for audit",
  });

  await api.actions.create({
    name: "slack-alerts",
    action: { selected: "script", subcontent: { command: ["/usr/local/bin/post-slack"] } },
    comment: "Posts to #alerts via webhook",
  });
  await api.actions.create({
    name: "pagerduty",
    action: { selected: "script", subcontent: { command: ["/usr/local/bin/pd-trigger"] } },
  });
  await api.notifications.create({
    name: "critical-page-oncall",
    enabled: true,
    condition: { type: "EQUALS", field: "severity", value: "critical" },
    actions: ["pagerduty", "slack-alerts"],
    // Frequency demo: at most 5 deliveries, 60s initial delay, every 5 min.
    frequency: { total: 5, delay: 60, every: 300 },
    comment: "Page oncall + post to Slack for criticals",
  });
  await api.notifications.create({
    name: "warning-slack-only",
    enabled: true,
    condition: { type: "EQUALS", field: "severity", value: "warning" },
    actions: ["slack-alerts"],
    // Business hours only, weekdays.
    time_constraints: {
      weekdays: [{ weekdays: [1, 2, 3, 4, 5] }],
      time: [{ from: "09:00", until: "18:00" }],
    },
  });

  await api.users.create({
    name: "alice",
    type: "local",
    roles: ["admin", "oncall"],
    groups: ["sre", "europe"],
    last_login: Math.floor(Date.now() / 1000) - 600,
    comment: "On-call lead",
  });
  await api.users.create({
    name: "bob",
    type: "local",
    roles: ["analyst"],
    groups: ["security"],
    last_login: Math.floor(Date.now() / 1000) - 86400,
  });
  await api.users.create({
    name: "carol",
    type: "ldap",
    roles: ["viewer"],
    groups: ["audit", "external"],
    last_login: Math.floor(Date.now() / 1000) - 5 * 86400,
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

  // Runtime settings: the SettingsPage is a tabbed UI (General /
  // Notifications / LDAP / Housekeeping), one card per catalogue entry.
  // The settings plugin publishes a typed `setting_form` catalogue in its
  // metadata.yaml with a `group:` per entry; the page buckets cards by
  // group and renders a typed input (Switch → toggle, Selector → dropdown,
  // String → text input, etc.). Seeding the catalogue keys exercises that
  // typed path in the screenshots.
  await api.settings.create({
    name: "default_auth_backend",
    value: "local",
    comment: "Backend that will be first in the list",
  });
  await api.settings.create({
    name: "local_users_enabled",
    value: true,
    comment: "Allow creating local users",
  });
  await api.settings.create({
    name: "metrics_enabled",
    value: true,
    comment: "Expose Prometheus metrics",
  });
  await api.settings.create({
    name: "anonymous_enabled",
    value: false,
    comment: "Allow anonymous login",
  });
  await api.settings.create({
    name: "ok_severities",
    value: ["ok", "success"],
    comment: "Severities that auto-close aggregates",
  });
  await api.settings.create({
    name: "notification_freq",
    value: "60s",
    comment: "Delay between repeat notifications",
  });
  await api.settings.create({
    name: "notification_retry",
    value: 3,
    comment: "Retries for failed notifications",
  });

  // LDAP + Housekeeping seeds — when the backend agent adds these to
  // setting_form in metadata.yaml, they'll render as typed cards inside
  // the LDAP / Housekeeping tabs. Until the backend lands, the records
  // still exist in the DB and surface under the Custom tab.
  await api.settings.create({
    name: "ldap.enabled",
    value: true,
    comment: "Enable the LDAP backend",
  });
  await api.settings.create({
    name: "ldap.host",
    value: "ldap://ldap.example.com",
    comment: "LDAP server URL",
  });
  await api.settings.create({
    name: "ldap.base_dn",
    value: "ou=people,dc=example,dc=com",
  });
  await api.settings.create({
    name: "housekeeping.cleanup_snooze",
    value: "48h",
    comment: "Retention for expired snoozes",
  });
  await api.settings.create({
    name: "housekeeping.cleanup_audit",
    value: "30d",
    comment: "Retention for audit-log rows",
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

  // Walk a rule through a few PATCH operations so its audit pane has
  // content when we screenshot the editor. The audit recorder in
  // internal/plugins/crud.go writes a row to the "audit" collection for
  // each mutation, keyed by object_id == rule uid. The frontend
  // AuditTimeline queries /api/v1/audit and renders one badge per row.
  const rules = (await api.rules.list()) as { uid?: string; name?: string }[];
  const tagProd = rules.find((r) => r.name === "tag-production");
  if (tagProd?.uid) {
    const u = tagProd.uid;
    // Three separate PATCHes — each becomes its own audit row with a
    // distinct field-list summary.
    await api.ctx.patch(`${server.baseURL}/api/v1/rule/${u}`, {
      headers: { Authorization: `Bearer ${api.token}` },
      data: { comment: "Mark prod-host alerts (refined wording)" },
    });
    await api.ctx.patch(`${server.baseURL}/api/v1/rule/${u}`, {
      headers: { Authorization: `Bearer ${api.token}` },
      data: { enabled: false },
    });
    await api.ctx.patch(`${server.baseURL}/api/v1/rule/${u}`, {
      headers: { Authorization: `Bearer ${api.token}` },
      data: { enabled: true, comment: "Mark prod-host alerts so dashboards filter cleanly" },
    });
  }

  // Bump the `duplicates` counter on a couple records so the Hits column
  // in the alerts table has content. In production this is incremented
  // by the aggregate-rule pipeline on hash collisions
  // (internal/pluginimpl/aggregaterule/plugin.go); for the tour we PATCH
  // it directly so the column has visible values regardless of which
  // aggregate rules happen to be configured.
  const recordsForHits = (await api.alerts.list()) as { uid?: string; host?: string }[];
  const dupes: Record<string, number> = {
    "srv-prod-1": 7,
    "srv-prod-3": 14,
    "edge-eu": 3,
    "edge-us": 21,
  };
  for (const r of recordsForHits) {
    if (!r.uid || !r.host) continue;
    const n = dupes[r.host];
    if (n) {
      await api.ctx.patch(`${server.baseURL}/api/v1/record/${r.uid}`, {
        headers: { Authorization: `Bearer ${api.token}` },
        data: { duplicates: n },
      });
    }
  }

  // Seed comments + acks on the srv-prod-1 record so the alert detail
  // drawer's CommentTimeline pane has content. Without this, the screenshot
  // shows the "No comments yet." empty state. Each comment carries the
  // record_uid of its parent alert and a discrete type/message; the
  // CommentTimeline component (web/src/features/alerts/CommentTimeline.tsx)
  // renders one badge per type.
  const records = (await api.alerts.list()) as { uid?: string; host?: string }[];
  const prod1 = records.find((r) => r.host === "srv-prod-1");
  if (prod1?.uid) {
    const baseEpoch = Math.floor(Date.now() / 1000) - 3600;
    await api.comments.create({
      record_uid: prod1.uid,
      type: "comment",
      message: "Investigating disk pressure — looks like log spam from /var/log/cron",
      date_epoch: baseEpoch,
      user: "alice",
    });
    await api.comments.create({
      record_uid: prod1.uid,
      type: "ack",
      message: "Acked, taking it.",
      date_epoch: baseEpoch + 600,
      user: "alice",
    });
    await api.comments.create({
      record_uid: prod1.uid,
      type: "esc",
      message: "Couldn't reproduce — escalating to platform.",
      date_epoch: baseEpoch + 1800,
      user: "alice",
    });
    await api.comments.create({
      record_uid: prod1.uid,
      type: "comment",
      message: "Rotated logs, see PR #4421.",
      date_epoch: baseEpoch + 2400,
      user: "bob",
    });
  }

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
  // Drawer doesn't reliably close on Escape between consecutive opens —
  // navigate fresh each time so the row click below hits the table, not
  // the drawer overlay.
  await goto("/web/rules");

  // The user's specific complaint: nested conditions/rules existed in the
  // backend but weren't visible anywhere in the tour. This drawer opens the
  // page-prod-criticals rule which carries a 3-level AND(OR, NOT, OR) tree;
  // the ConditionEditor's Builder mode renders the nested groups + NOT badge
  // recursively, proving the editor walks deeper than 1 level.
  await page.getByText("page-prod-criticals").click({ force: true });
  await shoot("21a-edit-rule-nested-condition");
  await goto("/web/rules");

  // Parent rule in the 3-level tree. With its name visible and the modifications
  // listed (SET region=edge), it's clear that this rule is the trunk; the
  // children would only run if this matches. The rules-list screenshot above
  // (04-rules) shows the three "tree-*" names sitting next to each other.
  await page.getByText("tree-root: tag-by-region").click({ force: true });
  await shoot("21b-edit-rule-parent");
  await goto("/web/rules");

  // 21c: audit pane visible at the bottom of the tag-production edit
  // drawer. The pre-screenshot seed PATCHed this rule 3 times, so the
  // AuditTimeline shows: created, edited (comment), edited (enabled),
  // edited (enabled+comment). Scroll into view so the new pane is in
  // frame.
  await page.getByText("tag-production").click({ force: true });
  // Scroll the drawer body to the bottom — the audit pane is the last
  // section after Modifications.
  const drawerBody = page.locator("[data-radix-scroll-area-viewport], section").last();
  await drawerBody.evaluate((el) => el.scrollIntoView({ block: "end" })).catch(() => undefined);
  await shoot("21c-edit-rule-audit-pane");
  await goto("/web/rules");

  // 21d: aggregate-rule edit drawer. Switch tabs first so the
  // "flapping-hosts" entry is visible to the row click. Captures the
  // fields/watch/throttle widget set the legacy UI exposed.
  await page.getByRole("tab", { name: /aggregates/i }).click({ force: true });
  await page.getByText("flapping-hosts").click({ force: true });
  await shoot("21d-edit-aggregate-rule");
  await goto("/web/rules");

  // 21h: roles table with permission badges (one badge per permission,
  // color-coded by responsibility).
  await goto("/web/admin/roles");
  await shoot("21h-roles-permission-badges");

  // 21i/21j/21k: snooze state tabs — Active / Upcoming / Expired. The
  // seeds above include one snooze for each bucket.
  await goto("/web/snoozes");
  await shoot("21i-snoozes-active-tab");
  await page.getByRole("tab", { name: /^upcoming/i }).click({ force: true });
  await shoot("21j-snoozes-upcoming-tab");
  await page.getByRole("tab", { name: /^expired/i }).click({ force: true });
  await shoot("21k-snoozes-expired-tab");

  // 21e: snooze edit drawer with the time-constraints widget filled in.
  // weekend-window is seeded with weekdays=[0,6] and time=22:00-06:00.
  // The TimeConstraints CollapsibleSection auto-opens when content is
  // present, but if it hasn't, click the header to make sure the new
  // DateTimeRangePicker chip is visible in the screenshot.
  await goto("/web/snoozes");
  await page.getByText("weekend-window").click({ force: true });
  // Click the section header if it's not already expanded. The header
  // exposes aria-expanded; we click only when it's "false" so we don't
  // re-collapse an already-open section.
  const tcHeader = page.getByRole("button", { name: /^Time constraints/i });
  await tcHeader.waitFor({ state: "visible" }).catch(() => undefined);
  const tcExpanded = await tcHeader.getAttribute("aria-expanded");
  if (tcExpanded === "false") {
    await tcHeader.click({ force: true });
  }
  await shoot("21e-edit-snooze-time-constraints");

  // 21e-popover: open the time-window picker so the screenshot captures
  // the new time-spinner popover. The picker chip carries an aria-label
  // combining "Time window 1 from / Time window 1 until (HH:MM – HH:MM)"
  // — match by the prefix.
  const timePickerChip = page.getByRole("button", {
    name: /^Time window 1 from \/ Time window 1 until/i,
  });
  if (await timePickerChip.count()) {
    await timePickerChip.first().click({ force: true });
    // Radix renders the popover via a portal; wait until the time input
    // inside it is in the DOM before shooting, otherwise the screenshot
    // captures the chip alone (before the portal mounts). The input is
    // a native <input type="time">, addressed directly so it doesn't
    // collide with the chip's identical aria-label substring.
    await page
      .locator('input[type="time"][aria-label="Time window 1 from"]')
      .waitFor({ state: "visible", timeout: 5000 })
      .catch(() => undefined);
    await shoot("21e-edit-snooze-time-constraints-popover");
    await page.keyboard.press("Escape");
  }
  await goto("/web/snoozes");

  // 21e-cal: open the *datetime* range picker (calendar variant). The
  // release-window-2026Q3 snooze has a datetime constraint, so its
  // editor surfaces the calendar+time popover when the chip is clicked.
  await page.getByRole("tab", { name: /^upcoming/i }).click({ force: true });
  await page.getByText("release-window-2026Q3").click({ force: true });
  const tcHeader2 = page.getByRole("button", { name: /^Time constraints/i });
  await tcHeader2.waitFor({ state: "visible" }).catch(() => undefined);
  const tcExpanded2 = await tcHeader2.getAttribute("aria-expanded");
  if (tcExpanded2 === "false") {
    await tcHeader2.click({ force: true });
  }
  const dtPickerChip = page.getByRole("button", {
    name: /^Date range 1 from \/ Date range 1 until/i,
  });
  if (await dtPickerChip.count()) {
    await dtPickerChip.first().click({ force: true });
    // Wait for the calendar grid to mount (react-day-picker renders the
    // days as a <table role="grid">) before shooting.
    await page
      .locator('table[role="grid"]')
      .waitFor({ state: "visible", timeout: 5000 })
      .catch(() => undefined);
    await shoot("21e-edit-snooze-datetime-popover");
    await page.keyboard.press("Escape");
  }
  await goto("/web/snoozes");

  // 21f: notification edit drawer showing the actions Combobox (badges)
  // + frequency editor + time-constraints. critical-page-oncall has all
  // three populated.
  await goto("/web/notifications");
  await page.getByText("critical-page-oncall").click({ force: true });
  await shoot("21f-edit-notification-full");
  await goto("/web/notifications");

  // 21g: user edit drawer showing the roles multi-select with badges.
  await goto("/web/admin/users");
  await page.getByText("alice").click({ force: true });
  await shoot("21g-edit-user-roles");
  await goto("/web/admin/users");

  await goto("/web/snoozes");
  await page.getByRole("button", { name: /^new$/i }).click({ force: true });
  await shoot("22-new-snooze-drawer");
  await page.keyboard.press("Escape");

  // Edit-snooze counterpart to 21-edit-rule-drawer. The original tour only
  // captured the empty "new" drawer; an existing snooze populates the
  // ConditionEditor + TTL + comment fields so the screenshot actually
  // illustrates the edit surface.
  await page.getByText("weekend-window").click({ force: true });
  await shoot("22a-edit-snooze-drawer");
  await page.keyboard.press("Escape");

  await goto("/web/notifications");
  await page.getByText("critical-page-oncall").click({ force: true });
  await shoot("23-edit-notification-drawer");
  await page.keyboard.press("Escape");

  await goto("/web/alerts");
  // Alerts moved from a detail-drawer to inline row expansion (chevron
  // toggle) — matching every other list page. The expanded panel renders
  // a JsonViewer (left) + CommentTimeline (right). The pre-screenshot
  // seed inserted comment/ack/esc events on srv-prod-1, so the timeline
  // pane now shows the populated badges instead of the empty state.
  // Target the chevron on the srv-prod-1 row (not the first chevron, since
  // the table is sorted by date_epoch desc and the most recent alert is
  // "auth").
  const prodRow = page.locator("tr", { hasText: "srv-prod-1" }).first();
  await prodRow.waitFor({ state: "attached" });
  const alertExpand = prodRow.getByRole("button", { name: /^Expand row /i });
  await alertExpand.click({ force: true });
  // The JsonViewer renders <pre> nodes synchronously; CommentTimeline
  // fetches comments async. Wait on the <pre> first.
  await page.locator("pre").first().waitFor({ state: "visible" });
  await shoot("24-alert-row-expanded");
  // Collapse so the bulk-ack screenshot below isn't crowded by the panel.
  await alertExpand.click({ force: true });

  // Row-actions menu: bulk-ack dialog
  await page.locator("thead").getByRole("checkbox").first().check({ force: true });
  await page.getByRole("button", { name: /acknowledge \(\d+\)/i }).click({ force: true });
  await shoot("25-bulk-ack-dialog");
  await page.keyboard.press("Escape");

  // Command palette — focus by tabbing past the topbar so we don't click a
  // table row. The keyboard listener is on `window`, so any element having
  // focus works.
  await goto("/web/alerts");
  await page.keyboard.press("Tab");
  await page.keyboard.press("Control+K");
  await shoot("26-command-palette");
  await page.keyboard.press("Escape");

  // ── 3b. Per-resource New + Edit drawers (consistent naming) ─────────────
  //
  // Spec-driven coverage: for every list-page resource the tour visits,
  // capture both the empty `New` drawer (named <resource>-drawer-new.png)
  // and a populated `Edit` drawer (named <resource>-drawer-edit.png).
  // Where the legacy section above already screenshots an analogous view
  // under a different name (e.g. 21-edit-rule-drawer), the consistently-
  // named capture below acts as the canonical one referenced by docs.
  //
  // Each helper navigates fresh to the list page so the previously-open
  // drawer overlay doesn't intercept the New-button click; the same
  // fresh-nav pattern is used by the earlier rule-drawer block above.
  const captureNew = async (route: string, file: string) => {
    await goto(route);
    await page.getByRole("button", { name: /^new$/i }).click({ force: true });
    await shoot(file);
    await page.keyboard.press("Escape");
  };
  const captureEditByText = async (route: string, rowText: string, file: string) => {
    await goto(route);
    await page.getByText(rowText, { exact: false }).first().click({ force: true });
    await shoot(file);
    await page.keyboard.press("Escape");
  };

  // Rules (Rules tab — RulesTreeTable). The tree row's click handler calls
  // onRowOpen which opens RuleEditor; same as the legacy 20/21 captures
  // but under the canonical name.
  await captureNew("/web/rules", "rules-drawer-new");
  await captureEditByText("/web/rules", "tag-production", "rules-drawer-edit");

  // Aggregate rules — must switch to the Aggregates tab before clicking New
  // or row.
  await goto("/web/rules");
  await page.getByRole("tab", { name: /aggregates/i }).click({ force: true });
  await page.getByRole("button", { name: /^new$/i }).click({ force: true });
  await shoot("aggregate-rules-drawer-new");
  await page.keyboard.press("Escape");
  await goto("/web/rules");
  await page.getByRole("tab", { name: /aggregates/i }).click({ force: true });
  await page.getByText("flapping-hosts").click({ force: true });
  await shoot("aggregate-rules-drawer-edit");
  await page.keyboard.press("Escape");

  // Snoozes
  await captureNew("/web/snoozes", "snoozes-drawer-new");
  await captureEditByText("/web/snoozes", "weekend-window", "snoozes-drawer-edit");

  // Notifications — Notifications tab (default).
  await captureNew("/web/notifications", "notifications-drawer-new");
  await captureEditByText(
    "/web/notifications",
    "critical-page-oncall",
    "notifications-drawer-edit",
  );

  // Actions — tab on the Notifications page. Switch tabs first so the
  // New-button + row click target the Actions list, not Notifications.
  await goto("/web/notifications");
  await page.getByRole("tab", { name: /^actions$/i }).click({ force: true });
  await page.getByRole("button", { name: /^new$/i }).click({ force: true });
  await shoot("actions-drawer-new");
  await page.keyboard.press("Escape");
  await goto("/web/notifications");
  await page.getByRole("tab", { name: /^actions$/i }).click({ force: true });
  await page.getByText("slack-alerts").click({ force: true });
  await shoot("actions-drawer-edit");
  await page.keyboard.press("Escape");

  // Users
  await captureNew("/web/admin/users", "users-drawer-new");
  await captureEditByText("/web/admin/users", "alice", "users-drawer-edit");

  // Roles
  await captureNew("/web/admin/roles", "roles-drawer-new");
  await captureEditByText("/web/admin/roles", "analyst", "roles-drawer-edit");

  // Widgets
  await captureNew("/web/admin/widgets", "widgets-drawer-new");
  await captureEditByText("/web/admin/widgets", "patlite-floor1", "widgets-drawer-edit");

  // Environments
  await captureNew("/web/admin/environments", "environments-drawer-new");
  await captureEditByText("/web/admin/environments", "production", "environments-drawer-edit");

  // KV
  await captureNew("/web/admin/kv", "kv-drawer-new");
  await captureEditByText("/web/admin/kv", "DEFAULT_RETENTION_DAYS", "kv-drawer-edit");

  // Settings — the page is now a tabbed catalogue
  // (General / Notifications / LDAP / Housekeeping), one card per
  // catalogue entry with label + description + typed input + per-card
  // Save/Reset. Capture each tab state so the screenshot set advertises
  // the variety of typed inputs (Switch, Selector, Arguments, String,
  // Number). Wait on a tab to render before clicking — the page renders
  // a Spinner until both the catalogue and the records list resolve.
  await goto("/web/admin/settings");
  await page.getByRole("tab", { name: /^general$/i }).waitFor({ state: "visible" });
  await shoot("13-admin-settings-general");
  await page.getByRole("tab", { name: /^ldap$/i }).click({ force: true });
  await shoot("13-admin-settings-ldap");
  await page.getByRole("tab", { name: /^housekeeping$/i }).click({ force: true });
  await shoot("13-admin-settings-housekeeping");

  // ── 3c. Inline row-expansion (chevron toggle) ───────────────────────────
  //
  // Recently-landed feature: every non-alert DataTable now exposes a
  // first-column chevron that toggles an inline RowDetailPanel
  // (JsonViewer + AuditTimeline). The Rules tab uses RulesTreeTable, so
  // the chevron only appears on the *Aggregates* tab, on Snoozes, and on
  // admin pages. We pick Snoozes because the seeded rows include a
  // populated time_constraints object — the JsonViewer pane therefore
  // has interesting content for the screenshot.
  //
  // Selector: DataTable renders the chevron as <button aria-label="Expand
  // row ${key}"> (see web/src/shared/ui/DataTable.tsx). Use the first
  // such button so we're stable against row order.
  await goto("/web/snoozes");
  // Wait for a real row to render. The Snoozes table is initially in a
  // loading state (skeleton rows have no chevron); the chevron only
  // appears once the data has resolved. Wait on the row text first so we
  // don't race the skeleton.
  await page.getByText("weekend-window").first().waitFor({ state: "attached" });
  // Selector: DataTable renders the chevron as <button aria-label="Expand
  // row ${key}"> (see web/src/shared/ui/DataTable.tsx). Use the first
  // such button — `force:true` works around opacity:0.55 on disabled
  // rows (which Playwright might consider non-actionable otherwise).
  const firstExpand = page.getByRole("button", { name: /^Expand row /i }).first();
  await firstExpand.waitFor({ state: "attached" });
  await firstExpand.click({ force: true });
  // The expanded panel renders a <pre> inside JsonViewer; wait for it so
  // the screenshot doesn't capture mid-animation. Audit fetch is async,
  // but the JsonViewer pane is rendered synchronously by React.
  await page.locator("pre").first().waitFor({ state: "visible" });
  await shoot("snoozes-row-expanded");

  // ── 4. Light theme variant of a couple of pages ─────────────────────────
  await goto("/web/alerts");
  await page.getByRole("button", { name: /switch to (light|dark) theme/i }).click({ force: true });
  await shoot("30-alerts-light");
  await goto("/web/dashboard");
  await shoot("31-dashboard-light");
});

// ── Mobile tour ──────────────────────────────────────────────────────────────
// A second, lighter pass at a phone viewport (390×844). Walks the top-level
// routes, ASSERTS no horizontal page overflow at 360-class widths (the core
// mobile-friendliness guarantee), exercises the bottom-nav "More" sheet and a
// full-screen editor sheet, and screenshots each with a `-mobile` suffix. Same
// SNOOZE_TOUR gate as the desktop tour above.
test("mobile tour: walk top-level routes at phone width", async ({
  page,
  api,
  server,
  adminAuth,
}) => {
  test.setTimeout(120_000);
  await page.setViewportSize({ width: 390, height: 844 });

  const cdp = await page.context().newCDPSession(page);
  const { writeFile } = await import("node:fs/promises");
  const shoot = async (name: string) => {
    const { data } = await cdp.send("Page.captureScreenshot", {
      format: "jpeg",
      quality: 80,
      captureBeyondViewport: false,
    });
    await writeFile(resolve(SHOTS_DIR, `${name}-mobile.jpg`), Buffer.from(data, "base64"));
  };
  // The mobile-friendliness contract: no page should scroll horizontally at
  // phone width. Allow 2px for sub-pixel rounding.
  const noOverflow = async (label: string) => {
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    expect(overflow, `horizontal overflow on ${label}`).toBeLessThanOrEqual(2);
  };
  const visit = async (route: string, name: string) => {
    await page.goto(server.baseURL + route);
    await page.waitForLoadState("networkidle").catch(() => {});
    await noOverflow(name);
    await shoot(name);
  };

  await visit("/web/login", "00-login");
  await adminAuth();

  // Compact seed so the data tables have rows that must collapse into cards.
  // Several environments so the alerts-page EnvironmentBar has multiple pills
  // (it must flow as chips on its own row, not stack one-per-line).
  await api.environments.create({ name: "production", color: "#3fb950" });
  await api.environments.create({ name: "staging", color: "#d4a017" });
  await api.environments.create({ name: "dev", color: "#4f8cff" });
  await api.rules.create({
    name: "tag-production",
    enabled: true,
    condition: { type: "MATCHES", field: "host", value: "^srv-prod-" },
    modifications: [["SET", "environment", "production"]],
  });
  await api.snoozes.create({
    name: "weekend-window",
    enabled: true,
    condition: { type: "EQUALS", field: "host", value: "noisy-1" },
  });
  await api.users.create({ name: "alice", type: "local", roles: ["admin"] });
  // Let the rule cache settle before ingesting alerts.
  await new Promise((r) => setTimeout(r, 400));
  await api.alerts.sendMany([
    {
      host: "srv-prod-1",
      message:
        "Disk usage above 90% on /var/log/app — sustained for 15m, projected full in ~2h; investigate log rotation and the nightly batch job before it pages on-call",
      severity: "critical",
      source: "prometheus",
    },
    { host: "srv-prod-2", message: "OOM kill: nginx", severity: "error", source: "syslog" },
    {
      host: "db-master",
      message: "Replication lag 12s",
      severity: "warning",
      source: "pg-replication",
    },
  ]);
  await new Promise((r) => setTimeout(r, 600));

  await visit("/web/alerts", "01-alerts");

  // Select an alert: the bulk-action bar must wrap its buttons (Acknowledge,
  // Close, Re-escalate, Comment) across the full width — never a horizontal
  // scroll. Per-row checkboxes carry aria-label "Select row <key>".
  const firstRowCheckbox = page.getByRole("checkbox", { name: /^Select row /i }).first();
  await firstRowCheckbox.waitFor({ state: "visible" }).catch(() => {});
  await firstRowCheckbox.check({ force: true });
  await noOverflow("alerts bulk-action bar");
  await shoot("01b-alerts-selected");
  await firstRowCheckbox.uncheck({ force: true });

  await visit("/web/dashboard", "02-dashboard");
  await visit("/web/snoozes", "03-snoozes");
  await visit("/web/rules", "04-rules");
  await visit("/web/notifications", "06-notifications");
  await visit("/web/admin/users", "08-admin-users");
  await visit("/web/admin/settings", "13-admin-settings");

  // Bottom-nav "More" sheet (overflow nav + theme toggle + log out).
  await page.goto(server.baseURL + "/web/alerts");
  await page.waitForLoadState("networkidle").catch(() => {});
  await page.getByRole("button", { name: /^more$/i }).click({ force: true });
  await page
    .getByRole("dialog", { name: /menu/i })
    .waitFor({ state: "visible" })
    .catch(() => {});
  await shoot("40-more-sheet");
  await page.keyboard.press("Escape");

  // Full-screen editor sheet on mobile.
  await page.goto(server.baseURL + "/web/rules");
  await page.waitForLoadState("networkidle").catch(() => {});
  await page.getByRole("button", { name: /^new$/i }).click({ force: true });
  await noOverflow("new-rule-sheet");
  await shoot("41-new-rule-sheet");
  await page.keyboard.press("Escape");
});
