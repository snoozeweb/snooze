import { describe, expect, it } from "vitest";
import { docsUrl } from "./docs";

describe("docsUrl", () => {
  it("joins a slug onto the published docs base", () => {
    expect(docsUrl("general/integrations/grafana")).toBe(
      "https://snoozeweb.github.io/snooze/general/integrations/grafana",
    );
  });

  it("tolerates a leading slash on the slug", () => {
    expect(docsUrl("/general/integrations/sending-alerts")).toBe(
      "https://snoozeweb.github.io/snooze/general/integrations/sending-alerts",
    );
  });
});
