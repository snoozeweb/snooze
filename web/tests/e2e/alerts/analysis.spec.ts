// web/tests/e2e/alerts/analysis.spec.ts
//
// The agentic analysis, end to end through a real server: the Analysis tab of
// the alert inspector (read, author, correct, remove), the analysed dot the
// alerts table paints beside the severity badge, and the dashboard's Analyses
// view with the Right-now tile that counts it.
//
// Two identities, because the whole point of the surface is that writing is
// gated on a permission an admin does NOT inherit:
//
//   - the WRITER holds `rw_protected` literally, and sees Edit / Remove /
//     Write analysis;
//   - the READER holds only `ro_record`, and sees the same analysis with no
//     way to touch it.
//
// The harness's own token is the root token (rw_all), which the server
// deliberately refuses for protected writes — so every analysis seeded through
// the API here is PUT with the writer's token, exactly as the browser would.
import type { Page } from "@playwright/test";
import { test, expect } from "../harness/fixtures";
import { loginAsAdmin } from "../harness/auth";
import type { SnoozeApi } from "../harness/api";

const WRITER_USER = "ana-writer";
const READER_USER = "ana-reader";
const PASSWORD = "analysis-pw";

/**
 * The writer's permission set.
 *
 * `rw_protected` is the literal permission `PUT /record/{uid}/agentic` demands
 * — the rw_all wildcard does not satisfy it
 * (internal/api/middleware/permission.go RequireLiteralPerm), which is why the
 * harness's root token cannot stand in for this user.
 *
 * The three read grants are spelled out per collection rather than given as
 * `ro_all`: the server's CRUD authorizer accepts `ro_all`
 * (internal/plugins/authz.go), but the SPA's own gate does not — only `rw_all`
 * is a wildcard client-side (lib/auth/permissions.ts), so an `ro_all` holder
 * lands on "Access denied" before any request is made. `ro_stats` is what the
 * Dashboard route asks for (app/layout/nav-items.ts).
 */
const WRITER_PERMS = ["ro_record", "rw_record", "ro_stats", "rw_protected"];

/** The reader: enough to see every alert and its analysis, and nothing else. */
const READER_PERMS = ["ro_record"];

type Doc = { uid: string; name?: string };

/** Creates a role once per worker; returns the existing one on a re-run. */
async function ensureRole(api: SnoozeApi, name: string, permissions: string[]): Promise<void> {
  const existing = (await api.roles.list()) as unknown as Doc[];
  if (existing.some((r) => r.name === name)) return;
  await api.roles.create({ name, description: "e2e agentic analysis", permissions, groups: [] });
}

/**
 * Creates a local user bound to `role`. The user plugin's WriteTransformer
 * bcrypt-hashes the plaintext password server-side, same as the web UI's
 * UserEditor (see auth/login.spec.ts).
 */
async function ensureUser(api: SnoozeApi, name: string, role: string): Promise<void> {
  const existing = (await api.users.list()) as unknown as Doc[];
  if (existing.some((u) => u.name === name)) return;
  await api.users.create({
    name,
    method: "local",
    enabled: true,
    password: PASSWORD,
    roles: [role],
    groups: [],
  });
}

/** Injects an alert and waits for its stored record uid. */
async function seedAlert(api: SnoozeApi, host: string, message: string): Promise<string> {
  await api.alerts.send({ host, message, severity: "critical", source: "e2e-analysis" });
  const deadline = Date.now() + 10_000;
  for (;;) {
    const rows = (await api.alerts.list()) as { uid?: string; host?: string }[];
    const hit = rows.find((r) => r.host === host);
    if (hit?.uid) return hit.uid;
    if (Date.now() >= deadline) throw new Error(`seedAlert: no record for host ${host}`);
    await new Promise((r) => setTimeout(r, 100));
  }
}

/** One minimal, schema-valid analysis body. */
function analysisBody(summary: string, confidence: "high" | "medium" | "low") {
  return {
    root_cause: { summary, confidence },
    remediation_plan: {
      steps: [{ action: "Restart the collector", risk: "low" }],
      automatable: false,
    },
    source: "e2e",
  };
}

/** PUTs an analysis with the writer's token — the root token cannot. */
async function putAnalysis(
  api: SnoozeApi,
  token: string,
  uid: string,
  body: Record<string, unknown>,
): Promise<void> {
  const res = await api.ctx.put(`${api.baseURL}/api/v1/record/${uid}/agentic`, {
    headers: { Authorization: `Bearer ${token}` },
    data: body,
  });
  if (!res.ok()) throw new Error(`put agentic: ${res.status()} ${await res.text()}`);
}

/** Signs the browser in as an arbitrary user (loginAsAdmin only injects a token). */
async function loginAs(page: Page, baseURL: string, token: string): Promise<void> {
  await loginAsAdmin(page, { baseURL, token });
}

test.describe("alert agentic analysis", () => {
  let writerToken = "";
  let readerToken = "";

  test.beforeEach(async ({ api }) => {
    await ensureRole(api, "analysis-writer", WRITER_PERMS);
    await ensureRole(api, "analysis-reader", READER_PERMS);
    await ensureUser(api, WRITER_USER, "analysis-writer");
    await ensureUser(api, READER_USER, "analysis-reader");
    writerToken = await api.loginLocal(WRITER_USER, PASSWORD);
    readerToken = await api.loginLocal(READER_USER, PASSWORD);
    await api.alerts.clear();
  });

  test("an rw_protected holder writes, corrects and removes an analysis", async ({
    page,
    api,
    server,
  }) => {
    const host = "srv-analysis-write";
    const summary = "The collector wedged on a full queue";
    const uid = await seedAlert(api, host, "queue wedged");

    await loginAs(page, server.baseURL, writerToken);
    await page.goto(`${server.baseURL}/web/alerts?tab=all&record=${uid}`);

    const drawer = page.getByRole("dialog", { name: host });
    await expect(drawer).toBeVisible();

    // ── Empty ───────────────────────────────────────────────────────────────
    // Nothing stored: no header line points at an analysis.
    await drawer.getByRole("tab", { name: "Analysis" }).click({ force: true });
    await expect(drawer.getByRole("button", { name: /Analysis ·/ })).toHaveCount(0);
    await expect(drawer.getByText("No analysis yet")).toBeVisible();

    // ── Author ──────────────────────────────────────────────────────────────
    await drawer.getByRole("button", { name: "Write analysis" }).click({ force: true });
    await drawer.getByLabel("Summary", { exact: true }).fill(summary);
    // Confidence is a Radix RadioGroup; each item is named through
    // aria-labelledby (a <label for> cannot target a button).
    await drawer.getByRole("radio", { name: "Low" }).click({ force: true });
    await drawer.getByLabel("Action", { exact: true }).fill("Drain the queue");
    // Radix Select portals its list to <body>, so the option is looked up on
    // the page rather than inside the drawer.
    await drawer.getByRole("combobox", { name: /^Risk/ }).click({ force: true });
    await page.getByRole("option", { name: "Low" }).click({ force: true });
    await drawer.getByRole("button", { name: "Save analysis" }).click({ force: true });

    // Confidence is said once, in the pane's byline — not on the tab.
    await expect(drawer.getByText("Low confidence")).toHaveCount(1);
    // Off the Analysis tab, the header points at it in one line — written
    // from the web UI, so credited to a person ("Analysis", not "AI
    // analysis"). On the tab itself the line steps aside: the pane says it.
    const pointer = drawer.getByRole("button", { name: `Analysis · ${summary}` });
    await expect(pointer).toBeHidden();
    await drawer.getByRole("tab", { name: "Timeline" }).click({ force: true });
    await expect(pointer).toBeVisible();

    // ── The dot in the table ────────────────────────────────────────────────
    // The mutation invalidates the alerts list, so the severity cell repaints
    // with the analysed dot as soon as the drawer is out of the way.
    await drawer.getByRole("button", { name: "Close panel" }).click({ force: true });
    await expect(drawer).toBeHidden();
    await expect(page.getByRole("img", { name: "Analysed · low confidence" })).toBeVisible();

    // ── Correct ─────────────────────────────────────────────────────────────
    const openDrawer = async () => {
      await page
        .locator("tr", { hasText: host })
        .first()
        .getByRole("button", { name: "View details" })
        .click({ force: true });
      await expect(drawer).toBeVisible();
      await drawer.getByRole("tab", { name: "Analysis" }).click({ force: true });
    };
    await openDrawer();

    await drawer.getByRole("button", { name: "Edit" }).click({ force: true });
    await drawer.getByRole("button", { name: "Add step" }).click({ force: true });
    await drawer.getByLabel("Action", { exact: true }).nth(1).fill("Drain the node first");
    await drawer.getByRole("combobox", { name: /^Risk/ }).nth(1).click({ force: true });
    await page.getByRole("option", { name: "Medium" }).click({ force: true });
    // Reorder through the keyboard-reachable button, not the drag handle: it
    // is the path every non-mouse user takes and the one the a11y bar rests on.
    await drawer.getByRole("button", { name: "Move step 2 up" }).click({ force: true });
    await drawer.getByRole("button", { name: "Save analysis" }).click({ force: true });
    await expect(drawer.getByRole("button", { name: "Edit" })).toBeVisible();

    // Reopen from scratch: the order has to survive the round trip, not just
    // the local form state.
    await drawer.getByRole("button", { name: "Close panel" }).click({ force: true });
    await expect(drawer).toBeHidden();
    await openDrawer();
    const movedStep = drawer.getByRole("listitem").filter({ hasText: "Drain the node first" });
    await expect(movedStep).toHaveText(/^1Drain the node first/);

    // ── Remove ──────────────────────────────────────────────────────────────
    // Behind the ⋯ menu beside Edit, and still confirmed by a dialog.
    await drawer.getByRole("button", { name: "More analysis actions" }).click({ force: true });
    await page.getByRole("menuitem", { name: "Remove" }).click({ force: true });
    const confirm = page.getByRole("dialog", { name: "Remove this analysis?" });
    await expect(confirm).toBeVisible();
    await confirm.getByRole("button", { name: "Remove analysis" }).click({ force: true });

    await expect(drawer.getByText("No analysis yet")).toBeVisible();
    await expect(drawer.getByRole("tab", { name: "Analysis" })).toBeVisible();
  });

  test("prev/next asks before it discards a draft, and never writes it to the next alert", async ({
    page,
    api,
    server,
  }) => {
    // Sorted by host below, not by the default date_epoch: two alerts seeded
    // in the same second tie on the timestamp, and "next row" has to be a
    // known direction for this test to mean anything. "draft" < "next".
    const hostB = "srv-analysis-next";
    const hostA = "srv-analysis-draft";
    const uidB = await seedAlert(api, hostB, "the other alert");
    const uidA = await seedAlert(api, hostA, "queue wedged");
    await putAnalysis(api, writerToken, uidA, analysisBody("Written by the loop", "high"));
    await putAnalysis(api, writerToken, uidB, analysisBody("A different conclusion", "medium"));

    // Every write to the subtree, whoever it is addressed to. The bug this
    // guards was silent: the PUT went out, to the wrong uid, and looked like a
    // successful save.
    const agenticWrites: string[] = [];
    page.on("request", (req) => {
      if (req.method() === "PUT" && req.url().includes("/agentic")) agenticWrites.push(req.url());
    });

    await loginAs(page, server.baseURL, writerToken);
    await page.goto(
      `${server.baseURL}/web/alerts?tab=all&orderby=host&asc=true&record=${uidA}&analysis=1`,
    );

    const drawerA = page.getByRole("dialog", { name: hostA });
    await expect(drawerA).toBeVisible();
    await drawerA.getByRole("button", { name: "Edit" }).click({ force: true });
    const summary = drawerA.getByLabel("Summary", { exact: true });
    await summary.fill("Half-written correction that belongs to A");

    // J pages to the next row from inside the drawer. Focus has to leave the
    // textarea first — the shortcut is suppressed while typing, by design.
    await drawerA.getByRole("tab", { name: /^Analysis/ }).click({ force: true });
    await page.keyboard.press("j");

    const confirm = page.getByRole("dialog", { name: "Discard this analysis draft?" });
    await expect(confirm).toBeVisible();
    await confirm.getByRole("button", { name: "Keep editing" }).click({ force: true });
    await expect(confirm).toBeHidden();

    // Declined: same alert, same draft, still in the URL.
    await expect(page).toHaveURL(new RegExp(`record=${uidA}`));
    await expect(summary).toHaveValue("Half-written correction that belongs to A");

    // Accepted: the drawer moves, and what it shows is B's own analysis — not
    // A's draft wearing B's uid. (Focus is put back where the dialog took it
    // from asynchronously, so park it deliberately before the key press.)
    await drawerA.getByRole("tab", { name: /^Analysis/ }).click({ force: true });
    await page.keyboard.press("j");
    await expect(confirm).toBeVisible();
    await confirm.getByRole("button", { name: "Discard draft" }).click({ force: true });

    await expect(page).toHaveURL(new RegExp(`record=${uidB}`));
    const drawerB = page.getByRole("dialog", { name: hostB });
    await expect(drawerB).toBeVisible();
    await expect(drawerB.getByText("A different conclusion").first()).toBeVisible();
    await expect(drawerB.getByLabel("Summary", { exact: true })).toHaveCount(0);

    // Nothing was ever saved: not to B, not to A.
    expect(agenticWrites).toEqual([]);
  });

  test("a reader sees the analysis and is offered no way to change it", async ({
    page,
    api,
    server,
  }) => {
    const host = "srv-analysis-read";
    const summary = "Disk filled with unrotated audit logs";
    const uid = await seedAlert(api, host, "disk full");
    await putAnalysis(api, writerToken, uid, analysisBody(summary, "high"));

    await loginAs(page, server.baseURL, readerToken);
    await page.goto(`${server.baseURL}/web/alerts?tab=all&record=${uid}&analysis=1`);

    const drawer = page.getByRole("dialog", { name: host });
    await expect(drawer).toBeVisible();
    // The legacy ?analysis=1 still opens the inspector straight onto the tab.
    const tab = drawer.getByRole("tab", { name: "Analysis" });
    await expect(tab).toHaveAttribute("aria-selected", "true");
    // Seeded through the API by a tool, so the byline says a model wrote it.
    await expect(drawer.getByText("AI analysis", { exact: true })).toBeVisible();

    // The content is all there…
    await expect(drawer.getByText(summary).first()).toBeVisible();
    await expect(drawer.getByText("Restart the collector")).toBeVisible();
    // …and none of the three write affordances is.
    await expect(drawer.getByRole("button", { name: "Edit" })).toHaveCount(0);
    await expect(drawer.getByRole("button", { name: "More analysis actions" })).toHaveCount(0);
    await expect(drawer.getByRole("button", { name: "Write analysis" })).toHaveCount(0);
  });

  test("the dashboard lists analysed alerts, filters them, and opens one", async ({
    page,
    api,
    server,
  }) => {
    const host = "srv-analysis-panel";
    const summary = "Replica fell behind after the failover";
    const uid = await seedAlert(api, host, "replication lag");
    await putAnalysis(api, writerToken, uid, analysisBody(summary, "low"));

    await loginAs(page, server.baseURL, writerToken);
    await page.goto(`${server.baseURL}/web/dashboard`);

    // ── The Right-now tile ──────────────────────────────────────────────────
    // The count of explained alerts sits beside the queue it is a share of,
    // and it is the second way into the view.
    const live = page.getByRole("region", { name: "Right now" });
    await expect(live.getByText("Analysed")).toBeVisible();
    await expect(live.getByText(/of \d+ open/)).toBeVisible();
    await live.getByRole("button", { name: /Analysed/ }).click({ force: true });
    await expect(page).toHaveURL(/view=analyses/);

    // A row is an article named by its alert; the heading inside it is the link.
    const row = page.getByRole("article", { name: new RegExp(host) });
    await expect(row).toBeVisible();

    // ── The view switch ─────────────────────────────────────────────────────
    // Back to the Overview and in again from the title row — the primary way
    // to reach the view, and the reason it is no longer a tab inside a card.
    const viewSwitch = page.getByRole("group", { name: "Dashboard view" });
    await viewSwitch.getByRole("button", { name: "Overview" }).click({ force: true });
    await expect(row).toHaveCount(0);
    await viewSwitch.getByRole("button", { name: "Analyses" }).click({ force: true });
    await expect(row).toBeVisible();
    // The view is live, not windowed: the time picker is not on screen.
    await expect(page.getByRole("button", { name: "1d" })).toHaveCount(0);

    // A confidence floor above the one level the list carries empties it — the
    // filters narrow the fetched rows client-side — and the empty offers the
    // way back.
    const confidence = page.getByRole("radiogroup", { name: "Confidence" });
    await confidence.getByRole("radio", { name: "Medium+" }).click({ force: true });
    await expect(row).toHaveCount(0);
    await expect(page.getByText("Nothing matches these filters")).toBeVisible();

    await page.getByRole("button", { name: "Clear filters" }).click({ force: true });
    await expect(row).toBeVisible();

    // The row's whole point is to be opened, on the tab it is about.
    await row.getByRole("link", { name: "Open alert" }).click({ force: true });
    await expect(page).toHaveURL(new RegExp(`record=${uid}`));
    await expect(page).toHaveURL(/pane=analysis/);
    const drawer = page.getByRole("dialog", { name: host });
    await expect(drawer).toBeVisible();
    await expect(drawer.getByRole("tab", { name: "Analysis" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
  });
});
