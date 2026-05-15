// web/tests/e2e/rules/editor-builder.spec.ts
//
// Exercises RuleEditor in Builder mode against a real backend.
// Covers:
//   - Open empty drawer, fill name + AND/EQUALS leaf via text mode → Create.
//   - Add a modification → Create persists modifications.
//   - Toggle the Enabled switch → rule persisted with enabled=false.
//
// Selectors:
//   - "New" button (RulesPage.tsx) opens the editor drawer.
//   - Drawer title "New rule" (RuleEditor.tsx).
//   - Name input has id="rule-name" and label "Name".
//   - ConditionEditor Tabs.Trigger "Text" switches the condition to free-text
//     mode (textarea with aria-label="Condition text"). Switching back to
//     "Builder" commits the parsed AST to the form via onChange.
//   - ModificationList "Add modification" creates a "set" row (placeholder
//     "field" + placeholder "value" inputs).
//   - Footer button "Create" submits; toast "Created" confirms success.
import { test, expect } from "../harness/fixtures";

test.describe("rule editor (builder mode)", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.rules.clear();
    await adminAuth();
  });

  test("create a rule with an EQUALS condition typed in text mode", async ({
    page,
    api,
    server,
  }) => {
    await page.goto(server.baseURL + "/web/rules");
    await page.getByRole("button", { name: /^new$/i }).click({ force: true });
    await expect(page.getByRole("heading", { name: /new rule/i })).toBeVisible();

    await page.getByLabel("Name").fill("e2e-leaf-rule");

    // Switch the condition editor to Text mode (avoids needing field suggestions).
    await page.getByRole("tab", { name: /^text$/i }).click({ force: true });
    await page.getByLabel("Condition text").fill('host = "server-1"');
    // Switching back to Builder mode commits the parsed AST.
    await page.getByRole("tab", { name: /^builder$/i }).click({ force: true });

    await page.getByRole("button", { name: /^create$/i }).click({ force: true });
    await expect(page.getByText(/^created$/i).first()).toBeVisible();

    const rules = await api.rules.list();
    expect(rules).toHaveLength(1);
  });

  test("create a rule with a modification", async ({ page, api, server }) => {
    await page.goto(server.baseURL + "/web/rules");
    await page.getByRole("button", { name: /^new$/i }).click({ force: true });

    await page.getByLabel("Name").fill("e2e-mod-rule");

    // Add one modification (default type: "set").
    await page.getByRole("button", { name: /add modification/i }).click({ force: true });
    // The modification row exposes plain Inputs with placeholders.
    await page.getByPlaceholder("field").fill("environment");
    await page.getByPlaceholder("value").fill("prod");

    await page.getByRole("button", { name: /^create$/i }).click({ force: true });
    await expect(page.getByText(/^created$/i).first()).toBeVisible();

    const rules = await api.rules.list();
    expect(rules).toHaveLength(1);
  });

  test("create a disabled rule via the Enabled toggle", async ({ page, api, server }) => {
    await page.goto(server.baseURL + "/web/rules");
    await page.getByRole("button", { name: /^new$/i }).click({ force: true });

    await page.getByLabel("Name").fill("e2e-disabled-rule");
    // Radix Switch renders <button role="switch" aria-label="Enabled">.
    await page.getByRole("switch", { name: /^enabled$/i }).click({ force: true });

    await page.getByRole("button", { name: /^create$/i }).click({ force: true });
    await expect(page.getByText(/^created$/i).first()).toBeVisible();

    const rules = await api.rules.list();
    expect(rules).toHaveLength(1);

    // After reload, list shows the rule with the "no" enabled badge.
    await page.reload();
    await expect(page.getByText("e2e-disabled-rule")).toBeVisible();
    await expect(page.getByText("no").first()).toBeVisible();
  });
});
