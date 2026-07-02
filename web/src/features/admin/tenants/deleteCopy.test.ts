import { describe, expect, it } from "vitest";
import { describeTenantDelete } from "./deleteCopy";

describe("describeTenantDelete", () => {
  it("names the tenant and spells out that its data becomes inaccessible", () => {
    const out = describeTenantDelete([{ display_name: "Acme", id: "acme" }]);
    expect(out.title).toBe("Delete tenant Acme?");
    expect(out.message).toMatch(/inaccessible/i);
    expect(out.message).toMatch(/cannot be undone/i);
  });

  it("covers the bulk case", () => {
    const out = describeTenantDelete([{ id: "a" }, { id: "b" }]);
    expect(out.title).toBe("Delete 2 tenants?");
    expect(out.message).toMatch(/inaccessible/i);
  });
});
