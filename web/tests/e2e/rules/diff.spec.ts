// web/tests/e2e/rules/diff.spec.ts
//
// Drives DiffSection (web/src/shared/ui/DiffSection.tsx):
//   - In create mode the Diff section is hidden (original===undefined).
//   - In edit mode the toggle button "Diff ▼" is rendered; clicking it
//     reveals a Diff that reflects the pending changes.
import { test, expect } from "../harness/fixtures";

test.describe("rule editor diff", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.rules.clear();
    await adminAuth();
  });

  test("Diff toggle is hidden on Create", async ({ page, server }) => {
    await page.goto(server.baseURL + "/web/rules");
    await page.getByRole("button", { name: /^new$/i }).click({ force: true });
    await expect(page.getByRole("heading", { name: /new rule/i })).toBeVisible();
    // The toggle reads "Diff ▼" / "Diff ▲" — its absence means create mode.
    await expect(page.getByRole("button", { name: /^diff/i })).toBeHidden();
  });

  test("Edit mode shows Diff toggle and expands the diff view", async ({
    page,
    api,
    server,
  }) => {
    await api.rules.create({
      name: "diff-target",
      enabled: true,
      condition: { type: "ALWAYS_TRUE" },
    });
    await page.goto(server.baseURL + "/web/rules");
    // Open the editor by clicking the row.
    await page.getByText("diff-target").click({ force: true });
    await expect(page.getByRole("heading", { name: /edit rule/i })).toBeVisible();

    // Make a pending change so the Diff has something to render.
    const comment = page.getByLabel("Comment");
    await comment.fill("post-create comment");

    const toggle = page.getByRole("button", { name: /^diff/i });
    await expect(toggle).toBeVisible();
    await toggle.click({ force: true });

    // The expanded Diff renders the comment line we added.
    await expect(page.getByText("post-create comment").first()).toBeVisible();
  });
});
