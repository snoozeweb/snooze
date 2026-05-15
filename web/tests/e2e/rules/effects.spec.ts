// web/tests/e2e/rules/effects.spec.ts
//
// End-to-end behaviour test: a created rule actually mutates incoming alerts
// according to its `modifications`. We seed a matching rule via the API,
// post an alert via /api/v1/alerts, then verify the resulting record has
// the modification's value applied. The UI's alert list view confirms it.
import { test, expect } from "../harness/fixtures";

test.describe("rule pipeline effects", () => {
  test.beforeEach(async ({ api, adminAuth }) => {
    await api.rules.clear();
    await api.alerts.clear();
    await adminAuth();
  });

  test("modification 'set environment=prod' is applied to matching alerts", async ({
    api,
    page,
    server,
  }) => {
    await api.rules.create({
      name: "prod-tagger",
      enabled: true,
      // ALWAYS_TRUE matches every alert.
      condition: { type: "ALWAYS_TRUE" },
      modifications: [{ type: "set", field: "environment", value: "prod" }],
    });

    // The syncer debounces collection-change events (~100ms default) before
    // calling Reload on the rule plugin. Wait a beat so the rule is cached
    // before we start ingesting alerts that should be tagged.
    await new Promise((r) => setTimeout(r, 400));

    await api.alerts.send({
      host: "srv-tagged",
      message: "test",
      severity: "info",
      source: "t",
    });

    await page.goto(server.baseURL + "/web/alerts");
    await expect(page.getByText("srv-tagged")).toBeVisible();
    // The environment column shows "prod" after the rule applies.
    await expect(page.getByText("prod").first()).toBeVisible();
  });
});
