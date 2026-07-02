import { describe, expect, it } from "vitest";
import { describeRoleDelete } from "./deleteCopy";

describe("describeRoleDelete", () => {
  it("warns that assigned users lose the role's permissions", () => {
    const out = describeRoleDelete([{ name: "oncall" }]);
    expect(out.title).toBe("Delete role oncall?");
    expect(out.message).toMatch(/lose the permissions/i);
    expect(out.message).toMatch(/cannot be undone/i);
  });

  it("covers the bulk case", () => {
    const out = describeRoleDelete([{ name: "a" }, { name: "b" }]);
    expect(out.title).toBe("Delete 2 roles?");
  });
});
