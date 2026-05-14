import { describe, expect, it } from "vitest";
import { decodeConditionQ } from "./decode";
import { encodeConditionQ } from "./serialize";
import type { Condition } from "./types";

describe("decodeConditionQ", () => {
  it("round-trips an EQUALS leaf", () => {
    const c: Condition = { type: "EQUALS", field: "host", value: "srv-1" };
    expect(decodeConditionQ(encodeConditionQ(c))).toEqual(c);
  });

  it("round-trips an AND of leaves", () => {
    const c: Condition = {
      type: "AND",
      args: [
        { type: "EQUALS", field: "host", value: "srv-1" },
        { type: "IN", field: "severity", value: ["error", "critical"] },
      ],
    };
    expect(decodeConditionQ(encodeConditionQ(c))).toEqual(c);
  });

  it("returns null on garbage input", () => {
    expect(decodeConditionQ("not~base64~json")).toBeNull();
    expect(decodeConditionQ("")).toBeNull();
  });
});
