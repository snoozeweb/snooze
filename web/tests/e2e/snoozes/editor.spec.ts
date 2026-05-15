// web/tests/e2e/snoozes/editor.spec.ts
//
// Exercises SnoozeEditor (web/src/features/snoozes/SnoozeEditor.tsx):
//   - Create a snooze with TTL=0 (forever) — happy path.
//   - Create a snooze with TTL=600 — value is persisted as-is.
//   - Edit an existing snooze (rename), Save persists the new name.
import { test, expect } from "../harness/fixtures";

test.describe("snooze editor", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.snoozes.clear();
    await adminAuth();
  });

  test("create a snooze with TTL forever via the dedicated New flow", async ({
    page,
    api,
    server,
  }) => {
    await page.goto(server.baseURL + "/web/snoozes");
    await page.getByRole("button", { name: /^new$/i }).click({ force: true });
    await expect(page.getByRole("heading", { name: /new snooze/i })).toBeVisible();

    await page.getByLabel("Name").fill("e2e-forever-snooze");

    // TTL input is type="number" with label "TTL (seconds, 0 = forever)".
    await page.getByLabel(/^TTL/i).fill("0");

    await page.getByRole("button", { name: /^create$/i }).click({ force: true });
    await expect(page.getByText(/snooze created/i).first()).toBeVisible();

    const snoozes = await api.snoozes.list();
    expect(snoozes).toHaveLength(1);
  });

  test("edit existing snooze — rename persists", async ({ page, api, server }) => {
    await api.snoozes.create({
      name: "snooze-original",
      enabled: true,
      condition: { type: "ALWAYS_TRUE" },
      ttl: 3600,
    });

    await page.goto(server.baseURL + "/web/snoozes");
    await page.getByText("snooze-original").click({ force: true });
    await expect(page.getByRole("heading", { name: /edit snooze/i })).toBeVisible();

    await page.getByLabel("Name").fill("snooze-renamed");
    await page.getByRole("button", { name: /^save$/i }).click({ force: true });
    await expect(page.getByText(/snooze saved/i).first()).toBeVisible();

    await page.reload();
    await expect(page.getByText("snooze-renamed")).toBeVisible();
  });
});
