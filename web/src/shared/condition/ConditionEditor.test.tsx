import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ConditionEditor } from "./ConditionEditor";
import type { Condition } from "@/lib/condition/types";

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>
      <TooltipProvider delay={0}>{children}</TooltipProvider>
    </QueryClientProvider>
  );
}

beforeEach(() => {
  mswServer.use(
    http.get("/api/v1/record", () =>
      HttpResponse.json({
        data: [{ host: "srv-1", severity: "info", environment: "prod" }],
        meta: { count: 1, limit: 50, offset: 0, total: 1 },
      }),
    ),
  );
});

describe("ConditionEditor", () => {
  it("renders the empty (ALWAYS_TRUE) state with an Add-filter button", () => {
    const onChange = vi.fn();
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ConditionEditor value={{ type: "ALWAYS_TRUE" }} onChange={onChange} plugin="record" />
      </Wrapper>,
    );
    expect(screen.getByText(/always/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /add filter/i })).toBeInTheDocument();
  });

  it("clicking Add filter emits an AND group with one empty EQUALS leaf", async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ConditionEditor value={{ type: "ALWAYS_TRUE" }} onChange={onChange} plugin="record" />
      </Wrapper>,
    );
    await user.click(screen.getByRole("button", { name: /add filter/i }));
    expect(onChange).toHaveBeenCalledWith({
      type: "AND",
      args: [{ type: "EQUALS", field: "", value: "" }],
    });
  });

  it("AND pill toggles to OR on click", async () => {
    const user = userEvent.setup();
    let last: Condition | undefined;
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ConditionEditor
          value={{ type: "AND", args: [{ type: "EQUALS", field: "host", value: "srv-1" }] }}
          onChange={(c) => (last = c)}
          plugin="record"
        />
      </Wrapper>,
    );
    await user.click(screen.getByRole("button", { name: "AND" }));
    expect(last?.type).toBe("OR");
  });

  it("removing the only leaf empties the group", async () => {
    const user = userEvent.setup();
    let last: Condition | undefined;
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ConditionEditor
          value={{ type: "AND", args: [{ type: "EQUALS", field: "host", value: "srv-1" }] }}
          onChange={(c) => (last = c)}
          plugin="record"
        />
      </Wrapper>,
    );
    await user.click(screen.getByRole("button", { name: /^remove$/i }));
    expect(last).toEqual({ type: "AND", args: [] });
  });
});
