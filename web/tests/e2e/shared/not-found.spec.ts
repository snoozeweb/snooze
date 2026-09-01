// web/tests/e2e/shared/not-found.spec.ts
//
// A stray/stale `/web/<unknown>` used to render the bare text "Not Found" on
// an empty canvas — no sidebar, no topbar, no way back. The route now falls
// through to a catch-all (app/router.tsx's webCatchAllRoute) that renders
// NotFound (shared/auth/NotFound.tsx) inside the normal AppShell, in both
// themes.
import { test, expect } from "../harness/fixtures";

test.describe("404 inside the shell", () => {
  test.beforeEach(async ({ adminAuth }) => {
    await adminAuth();
  });

  test("a stale /web/<unknown> link keeps the shell and offers a way back", async ({
    page,
    server,
  }) => {
    await page.goto(server.baseURL + "/web/this-page-does-not-exist");

    // The shell survives: sidebar nav is still present, not a blank canvas.
    await expect(page.getByRole("navigation")).toBeVisible();

    await expect(page.getByText(/page not found/i)).toBeVisible();
    const description = page.getByText(/this-page-does-not-exist/);
    await expect(description).toBeVisible();
    await expect(description).toContainText("⌘K");

    const backButton = page.getByRole("button", { name: /back to/i });
    await expect(backButton).toBeVisible();
    await backButton.click({ force: true });
    await expect(page).not.toHaveURL(/this-page-does-not-exist/);
  });

  test("renders inside the shell in both themes", async ({ page, server }) => {
    for (const theme of ["dark", "light"] as const) {
      await page.addInitScript((t) => {
        window.localStorage.setItem("snooze.theme", t);
      }, theme);
      await page.goto(server.baseURL + "/web/also-not-a-real-page");
      await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
      await expect(page.getByRole("navigation")).toBeVisible();
      await expect(page.getByText(/page not found/i)).toBeVisible();
    }
  });
});
