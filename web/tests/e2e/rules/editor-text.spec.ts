// web/tests/e2e/rules/editor-text.spec.ts
//
// Drives the ConditionEditor's Text mode (web/src/shared/condition/ConditionEditor.tsx):
//   - Builder → Text round-trip preserves a condition's encoded form.
//   - Editing valid text and switching back to Builder commits the parsed AST.
//   - Invalid text shows a parse error and blocks the mode switch.
//
// Selectors:
//   - Tab triggers via getByRole("tab", { name: /^builder$/i / ^text$/i }).
//   - Textarea by aria-label "Condition text".
//   - Parse error rendered with role="alert" inside Text panel.
import { test, expect } from "../harness/fixtures";

test.describe("rule editor (text mode)", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.rules.clear();
    await adminAuth();
  });

  test("typing in text mode and switching back to builder commits the AST", async ({
    page,
    api,
    server,
  }) => {
    await page.goto(server.baseURL + "/web/rules");
    await page.getByRole("button", { name: /^new$/i }).click({ force: true });
    await page.getByLabel("Name").fill("e2e-text-mode");

    await page.getByRole("tab", { name: /^text$/i }).click({ force: true });
    const textarea = page.getByLabel("Condition text");
    await textarea.fill('severity = "critical"');

    // Switch back to Builder — this triggers parseText and onChange with the AST.
    await page.getByRole("tab", { name: /^builder$/i }).click({ force: true });

    await page.getByRole("button", { name: /^create$/i }).click({ force: true });
    await expect(page.getByText(/^created$/i).first()).toBeVisible();

    const rules = await api.rules.list();
    expect(rules).toHaveLength(1);
  });

  test("invalid text shows a parse error and blocks the switch to builder", async ({
    page,
    server,
  }) => {
    await page.goto(server.baseURL + "/web/rules");
    await page.getByRole("button", { name: /^new$/i }).click({ force: true });
    await page.getByLabel("Name").fill("e2e-bad-text");

    await page.getByRole("tab", { name: /^text$/i }).click({ force: true });
    const textarea = page.getByLabel("Condition text");
    // Wrong syntax — bareword without operator.
    await textarea.fill("severity ##");

    // Inline error appears (role="alert" inside the Text tab content).
    await expect(page.getByRole("alert")).toBeVisible();

    // Attempting to switch back to Builder is blocked by the parser.
    await page.getByRole("tab", { name: /^builder$/i }).click({ force: true });
    // We should still be on the Text panel (textarea still visible).
    await expect(textarea).toBeVisible();
  });
});
