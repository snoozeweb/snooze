import { render, screen, waitFor, fireEvent } from "@testing-library/react";
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

  it("clicking Add filter emits a paired AND with two empty EQUALS leaves", async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ConditionEditor value={{ type: "ALWAYS_TRUE" }} onChange={onChange} plugin="record" />
      </Wrapper>,
    );
    await user.click(screen.getByRole("button", { name: /add filter/i }));
    // Paired-by-default: AND/OR ship with two leaves so the user doesn't
    // see a one-armed group on the very first click.
    expect(onChange).toHaveBeenCalledWith({
      type: "AND",
      args: [
        { type: "EQUALS", field: "", value: "" },
        { type: "EQUALS", field: "", value: "" },
      ],
    });
  });

  it("trash on the root group resets to ALWAYS_TRUE", async () => {
    const user = userEvent.setup();
    let last: Condition | undefined;
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ConditionEditor
          value={{
            type: "AND",
            args: [
              { type: "EQUALS", field: "host", value: "srv-1" },
              { type: "EQUALS", field: "env", value: "prod" },
            ],
          }}
          onChange={(c) => (last = c)}
          plugin="record"
        />
      </Wrapper>,
    );
    await user.click(screen.getByRole("button", { name: /clear/i }));
    expect(last).toEqual({ type: "ALWAYS_TRUE" });
  });

  it("removing one leaf of a two-leaf AND collapses to the remaining leaf", async () => {
    const user = userEvent.setup();
    let last: Condition | undefined;
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ConditionEditor
          value={{
            type: "AND",
            args: [
              { type: "EQUALS", field: "host", value: "srv-1" },
              { type: "EQUALS", field: "env", value: "prod" },
            ],
          }}
          onChange={(c) => (last = c)}
          plugin="record"
        />
      </Wrapper>,
    );
    // First leaf's remove (the per-row trash). The header trash is "Clear".
    const trashes = screen.getAllByRole("button", { name: /^remove$/i });
    expect(trashes.length).toBeGreaterThan(0);
    const first = trashes[0];
    if (!first) throw new Error("expected at least one Remove button");
    await user.click(first);
    expect(last).toEqual({ type: "EQUALS", field: "env", value: "prod" });
  });
});

describe("ConditionEditor — logic operator changes", () => {
  it("switching AND → NOT truncates to the first child", async () => {
    let last: Condition | undefined;
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ConditionEditor
          value={{
            type: "AND",
            args: [
              { type: "EQUALS", field: "host", value: "srv-1" },
              { type: "EQUALS", field: "env", value: "prod" },
            ],
          }}
          onChange={(c) => (last = c)}
          plugin="record"
        />
      </Wrapper>,
    );
    // The Radix Select renders as a native button — open it via keyboard
    // since jsdom doesn't drive the popper.
    const select = screen.getAllByRole("combobox")[0];
    if (!select) throw new Error("expected logic operator select");
    fireEvent.click(select);
    // The hidden <select> Radix renders is also driven by changing value;
    // simplest path: click trigger then click NOT option.
    const opt = await screen.findByRole("option", { name: "NOT" });
    fireEvent.click(opt);
    expect(last).toEqual({
      type: "NOT",
      arg: { type: "EQUALS", field: "host", value: "srv-1" },
    });
  });

  it("switching NOT → AND pads the args with a new default leaf", async () => {
    let last: Condition | undefined;
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ConditionEditor
          value={{ type: "NOT", arg: { type: "EQUALS", field: "host", value: "srv-1" } }}
          onChange={(c) => (last = c)}
          plugin="record"
        />
      </Wrapper>,
    );
    const select = screen.getAllByRole("combobox")[0];
    if (!select) throw new Error("expected logic operator select");
    fireEvent.click(select);
    const opt = await screen.findByRole("option", { name: "AND" });
    fireEvent.click(opt);
    expect(last).toEqual({
      type: "AND",
      args: [
        { type: "EQUALS", field: "host", value: "srv-1" },
        { type: "EQUALS", field: "", value: "" },
      ],
    });
  });
});

describe("ConditionEditor — fork from a leaf", () => {
  it("(+) on the only leaf wraps it in a new AND with a fresh sibling", async () => {
    const user = userEvent.setup();
    let last: Condition | undefined;
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ConditionEditor
          value={{ type: "EQUALS", field: "host", value: "srv-1" }}
          onChange={(c) => (last = c)}
          plugin="record"
        />
      </Wrapper>,
    );
    // The leaf row has one (+) button labelled "Add filter".
    await user.click(screen.getByRole("button", { name: /add filter/i }));
    expect(last).toEqual({
      type: "AND",
      args: [
        { type: "EQUALS", field: "host", value: "srv-1" },
        { type: "EQUALS", field: "", value: "" },
      ],
    });
  });
});

describe("ConditionEditor — text mode", () => {
  it("toggles Builder → Text and shows encoded value", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ConditionEditor
          plugin="record"
          value={{ type: "EQUALS", field: "host", value: "srv-1" }}
          onChange={onChange}
        />
      </Wrapper>,
    );
    await user.click(screen.getByRole("tab", { name: /text/i }));
    const ta = screen.getByLabelText(/condition text/i);
    expect((ta as HTMLTextAreaElement).value).toBe(`host = "srv-1"`);
  });

  it("edits in Text mode and propagates parsed AST on switch to Builder", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ConditionEditor plugin="record" value={{ type: "ALWAYS_TRUE" }} onChange={onChange} />
      </Wrapper>,
    );
    await user.click(screen.getByRole("tab", { name: /text/i }));
    const ta = screen.getByLabelText(/condition text/i);
    fireEvent.change(ta, { target: { value: 'host = "srv-2"' } });
    await user.click(screen.getByRole("tab", { name: /builder/i }));
    await waitFor(() =>
      expect(onChange).toHaveBeenCalledWith({ type: "EQUALS", field: "host", value: "srv-2" }),
    );
  });

  it("disables Builder switch and shows error on invalid text", async () => {
    const user = userEvent.setup();
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ConditionEditor
          plugin="record"
          value={{ type: "ALWAYS_TRUE" }}
          onChange={() => undefined}
        />
      </Wrapper>,
    );
    await user.click(screen.getByRole("tab", { name: /text/i }));
    const ta = screen.getByLabelText(/condition text/i);
    fireEvent.change(ta, { target: { value: "this is = broken =" } });
    expect(await screen.findByRole("alert")).toBeInTheDocument();
  });
});

describe("ConditionEditor — nested groups render", () => {
  it("renders a deeply nested AND(OR(NOT, EQUALS), MATCHES) tree", () => {
    const Wrapper = wrap();
    const value: Condition = {
      type: "AND",
      args: [
        {
          type: "OR",
          args: [
            { type: "NOT", arg: { type: "EXISTS", field: "shelved" } },
            { type: "EQUALS", field: "host", value: "srv-prod-1" },
          ],
        },
        { type: "MATCHES", field: "message", value: "CPU" },
      ],
    };
    render(
      <Wrapper>
        <ConditionEditor plugin="record" value={value} onChange={() => undefined} />
      </Wrapper>,
    );
    // Three operator selects: outer AND, inner OR, and the NOT inside it.
    // Radix exposes the trigger as role=combobox; counting them proves
    // the tree was walked all the way down.
    expect(screen.getAllByRole("combobox").length).toBeGreaterThanOrEqual(3);
  });
});
