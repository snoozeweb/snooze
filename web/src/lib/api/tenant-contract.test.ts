// web/src/lib/api/tenant-contract.test.ts
import { describe, it, expect } from "vitest";

// Type-only import: pulls the compile-only assertions into this test's
// type graph (so `tsc --noEmit` checks them) without executing the
// `declare const` statements at runtime.
import type {} from "./tenant-contract.test-types";

describe("tenant API contract types", () => {
  it("types.gen.ts exports Tenant and LoginRequest.org (compile-only proof)", () => {
    // The real assertion is that this file compiles. The runtime test just
    // ensures vitest includes the file so coverage is tracked.
    expect(true).toBe(true);
  });
});
