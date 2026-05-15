// web/tests/e2e/shared/theme.spec.ts
//
// Verifies the theme toggle in the topbar:
//   - Default to dark theme on first visit.
//   - Clicking the toggle switches to light theme.
//   - The choice persists across reload.
import { test, expect } from "../harness/fixtures";

test.describe("topbar theme toggle", () => {
  test.beforeEach(async ({ adminAuth }) => {
    await adminAuth();
  });

  test("toggle switches theme and persists across reload", async ({ page, server }) => {
    await page.goto(server.baseURL + "/web/alerts");

    // useTheme attribute is reflected on <html data-theme="dark|light">.
    const html = page.locator("html");
    await expect(html).toHaveAttribute("data-theme", /dark|light/);
    const initial = await html.getAttribute("data-theme");

    // Click the toggle by its accessible label.
    await page
      .getByRole("button", { name: /switch to (light|dark) theme/i })
      .click({ force: true });
    const afterToggle = await html.getAttribute("data-theme");
    expect(afterToggle).not.toBe(initial);

    // Reload and confirm the new theme persists.
    await page.reload();
    await expect(page.locator("html")).toHaveAttribute("data-theme", afterToggle ?? "");
  });
});
