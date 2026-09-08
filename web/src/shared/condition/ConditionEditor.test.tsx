import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useState } from "react";
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

  it("clicking Add filter emits a single empty EQUALS leaf", async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ConditionEditor value={{ type: "ALWAYS_TRUE" }} onChange={onChange} plugin="record" />
      </Wrapper>,
    );
    await user.click(screen.getByRole("button", { name: /add filter/i }));
    expect(onChange).toHaveBeenCalledWith({ type: "EQUALS", field: "", value: "" });
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

describe("ConditionEditor — Text mode", () => {
  it("propagates a valid Text-mode edit via onChange without switching back to Builder", async () => {
    // The bug: typing a condition in Text mode and clicking Save (never touching
    // Builder) silently discarded the edit because onChange was never called.
    const changes: Condition[] = [];
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ConditionEditor
          value={{ type: "EQUALS", field: "host", value: "srv-1" }}
          onChange={(c) => changes.push(c)}
          plugin="record"
        />
      </Wrapper>,
    );
    await userEvent.click(screen.getByRole("tab", { name: /text/i }));
    const textarea = await screen.findByLabelText(/condition text/i);
    fireEvent.change(textarea, { target: { value: "host = srv-9" } });
    // The parsed edit must reach the form immediately — no Builder round-trip.
    expect(changes.length).toBeGreaterThan(0);
    expect(changes[changes.length - 1]).toMatchObject({ field: "host", value: "srv-9" });
  });

  it("does not call onChange while the Text is unparseable (keeps the last valid AST)", async () => {
    const changes: Condition[] = [];
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ConditionEditor
          value={{ type: "EQUALS", field: "host", value: "srv-1" }}
          onChange={(c) => changes.push(c)}
          plugin="record"
        />
      </Wrapper>,
    );
    await userEvent.click(screen.getByRole("tab", { name: /text/i }));
    const textarea = await screen.findByLabelText(/condition text/i);
    fireEvent.change(textarea, { target: { value: "host = = broken" } });
    expect(changes).toHaveLength(0);
    expect(screen.getByRole("alert")).toBeInTheDocument();
  });
});

describe("ConditionEditor — logic operator changes", () => {
  it("switching a multi-child AND → NOT negates the whole group (no child dropped)", async () => {
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
    const opt = await screen.findByRole("option", { name: "NOT" });
    fireEvent.click(opt);
    // Previously this discarded the second clause; now it preserves every
    // sub-condition by negating the group.
    expect(last).toEqual({
      type: "NOT",
      arg: {
        type: "AND",
        args: [
          { type: "EQUALS", field: "host", value: "srv-1" },
          { type: "EQUALS", field: "env", value: "prod" },
        ],
      },
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

describe("ConditionEditor — boolean operand on a string-shaped operator", () => {
  // Regression. EQUALS/NOT_EQUALS also carry a real boolean operand — that is
  // what the text DSL's `batch = true` parses to (see LeafBoolOp). Switching
  // the operator stashed that raw value and handed it to the next
  // string-shaped operator, so the builder emitted nodes like
  // `{ type: "SEARCH", value: true }`. The editor re-encodes the AST on every
  // change to keep the Text tab in sync, and `encodeText` -> `quoteString`
  // then threw `s.replace is not a function` mid-render, taking the whole
  // editor down. Driven through the real ConditionEditor precisely because
  // that re-encode is the thing that blew up.
  function Controlled({ initial }: { initial: Condition }) {
    const [value, setValue] = useState<Condition>(initial);
    return (
      <>
        <ConditionEditor value={value} onChange={setValue} plugin="record" />
        <output data-testid="ast">{JSON.stringify(value)}</output>
      </>
    );
  }

  /**
   * The leaf's operator dropdown. The field <Input> carries a `list` (the
   * suggestions datalist), which also maps to role=combobox and loads
   * asynchronously — so pick the Radix trigger by element rather than by
   * document order.
   */
  function operatorSelect(): HTMLElement {
    const trigger = screen.getAllByRole("combobox").find((el) => el.tagName === "BUTTON");
    if (!trigger) throw new Error("expected the leaf operator select");
    return trigger;
  }

  async function switchOperatorTo(label: string) {
    fireEvent.click(operatorSelect());
    fireEvent.click(await screen.findByRole("option", { name: label }));
  }

  it.each([
    ["search", "SEARCH"],
    ["contains", "CONTAINS"],
    ["matches", "MATCHES"],
  ])("carries a boolean EQUALS operand into %s as text", async (label, type) => {
    const Wrapper = wrap();
    render(
      <Wrapper>
        <Controlled initial={{ type: "EQUALS", field: "batch", value: true }} />
      </Wrapper>,
    );
    await switchOperatorTo(label);

    const ast = JSON.parse(screen.getByTestId("ast").textContent ?? "null") as Condition;
    expect(ast.type).toBe(type);
    expect("value" in ast ? ast.value : undefined).toBe("true");
    // The editor is still standing: encodeText ran on the new AST (the Text
    // tab is fed from it) instead of throwing out of the render.
    expect(screen.getByRole("tab", { name: "Text" })).toBeInTheDocument();
  });

  it("prints the coerced operand in the Text tab", async () => {
    const user = userEvent.setup();
    const Wrapper = wrap();
    render(
      <Wrapper>
        <Controlled initial={{ type: "EQUALS", field: "batch", value: true }} />
      </Wrapper>,
    );
    await switchOperatorTo("search");
    await user.click(screen.getByRole("tab", { name: "Text" }));
    expect(screen.getByRole("textbox")).toHaveValue('"true"');
  });
});
