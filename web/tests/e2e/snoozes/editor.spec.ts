// web/tests/e2e/snoozes/editor.spec.ts
//
// Exercises SnoozeEditor (web/src/features/snoozes/SnoozeEditor.tsx):
//   - Create a snooze without expiration — happy path (no TTL field; the
//     legacy Python era didn't have one and the Go backend doesn't
//     enforce it on the snooze collection, so we removed it from the
//     form).
//   - Edit an existing snooze (rename), Save persists the new name.
import { test, expect } from "../harness/fixtures";

test.describe("snooze editor", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.snoozes.clear();
    await adminAuth();
  });

  test("create a snooze via the dedicated New flow", async ({ page, api, server }) => {
    await page.goto(server.baseURL + "/web/snoozes");
    await page.getByRole("button", { name: /^new$/i }).click({ force: true });
    await expect(page.getByRole("heading", { name: /new snooze/i })).toBeVisible();

    await page.getByLabel("Name").fill("e2e-forever-snooze");

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
