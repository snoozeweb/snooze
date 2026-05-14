import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import axe from "axe-core";
import type { AxeResults } from "axe-core";
import { Button } from "@/shared/ui/Button";
import { Card } from "@/shared/ui/Card";
import { Input } from "@/shared/ui/Input";

async function runAxe(node: Element): Promise<AxeResults> {
  return new Promise((resolve, reject) => {
    axe.run(node, (err: Error | null, result: AxeResults) => {
      if (err) reject(err);
      else resolve(result);
    });
  });
}

describe("a11y smoke", () => {
  it("Button + labelled Input + Card have no axe violations", async () => {
    const { container } = render(
      <Card padded>
        <label htmlFor="a11y-x">Name</label>
        <Input id="a11y-x" />
        <Button>Save</Button>
      </Card>,
    );
    const result = await runAxe(container);
    if (result.violations.length > 0) {
      console.error(JSON.stringify(result.violations, null, 2));
    }
    expect(result.violations).toHaveLength(0);
  });
});
