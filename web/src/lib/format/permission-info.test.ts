import { describe, expect, it } from "vitest";
import { permissionDescription } from "./permission-info";

describe("permissionDescription", () => {
  it("describes ro_/rw_ resource permissions from the prefix", () => {
    expect(permissionDescription("ro_rule")).toMatch(/read-only.*rules/i);
    expect(permissionDescription("rw_rule")).toMatch(/read and write.*rules/i);
  });

  it("maps known resource tokens to friendly labels", () => {
    expect(permissionDescription("ro_record")).toMatch(/alerts/i);
    expect(permissionDescription("rw_secret")).toMatch(/credentials/i);
    // Multi-word plugin tokens get spelled out rather than reading as run-ons.
    expect(permissionDescription("rw_aggregaterule")).toMatch(/aggregate rules/i);
    expect(permissionDescription("ro_savedsearch")).toMatch(/saved searches/i);
  });

  it("keeps notification and action as distinct grants", () => {
    // `action` is a separate plugin/permission — rw_notification must not imply it.
    expect(permissionDescription("rw_notification")).not.toMatch(/action/i);
    expect(permissionDescription("rw_action")).toMatch(/actions/i);
  });

  it("has explicit copy for wildcard and can_ permissions", () => {
    expect(permissionDescription("rw_all")).toMatch(/every resource|superuser/i);
    expect(permissionDescription("can_comment")).toMatch(/comment/i);
    expect(permissionDescription("can_escalate")).toMatch(/escalate/i);
  });

  it("returns undefined for an unrecognised permission with no known prefix", () => {
    expect(permissionDescription("bananas")).toBeUndefined();
  });
});
