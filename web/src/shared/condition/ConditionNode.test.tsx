import { render, screen, fireEvent } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ConditionNode } from "./ConditionNode";
import type { Condition } from "@/lib/condition/types";

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>
      <TooltipProvider delay={0}>{children}</TooltipProvider>
    </QueryClientProvider>
  );
}

describe("ConditionNode — duplicate button", () => {
  it("duplicate button is absent when onDuplicate is not provided (root node)", () => {
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ConditionNode
          value={{ type: "EQUALS", field: "host", value: "srv-1" }}
          fieldOptions={[]}
          onChange={() => undefined}
          isRoot
        />
      </Wrapper>,
    );
    expect(screen.queryByRole("button", { name: /duplicate/i })).not.toBeInTheDocument();
  });

  it("duplicate button is present when onDuplicate is provided (non-root leaf)", () => {
    const onDuplicate = vi.fn();
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ConditionNode
          value={{ type: "EQUALS", field: "host", value: "srv-1" }}
          fieldOptions={[]}
          onChange={() => undefined}
          onDuplicate={onDuplicate}
        />
      </Wrapper>,
    );
    const btn = screen.getByRole("button", { name: /duplicate/i });
    expect(btn).toBeInTheDocument();
    fireEvent.click(btn);
    expect(onDuplicate).toHaveBeenCalledOnce();
  });

  it("duplicating a leaf inside an AND inserts a clone right after the original", async () => {
    const user = userEvent.setup();
    let last: Condition | undefined;
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ConditionNode
          value={{
            type: "AND",
            args: [
              { type: "EQUALS", field: "host", value: "srv-1" },
              { type: "EQUALS", field: "env", value: "prod" },
            ],
          }}
          fieldOptions={[]}
          onChange={(c) => (last = c)}
          isRoot
        />
      </Wrapper>,
    );
    // Each child leaf has a Duplicate button.
    const duplicates = screen.getAllByRole("button", { name: /^duplicate$/i });
    expect(duplicates.length).toBe(2);
    // Click the first child's duplicate.
    await user.click(duplicates[0]!);
    expect(last).toEqual({
      type: "AND",
      args: [
        { type: "EQUALS", field: "host", value: "srv-1" },
        { type: "EQUALS", field: "host", value: "srv-1" }, // clone inserted at i+1
        { type: "EQUALS", field: "env", value: "prod" },
      ],
    });
  });

  it("duplicating a leaf inside an OR inserts a clone right after the original", () => {
    let last: Condition | undefined;
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ConditionNode
          value={{
            type: "OR",
            args: [
              { type: "EQUALS", field: "a", value: "x" },
              { type: "EQUALS", field: "b", value: "y" },
            ],
          }}
          fieldOptions={[]}
          onChange={(c) => (last = c)}
          isRoot
        />
      </Wrapper>,
    );
    // Click the second child's duplicate.
    const duplicates = screen.getAllByRole("button", { name: /^duplicate$/i });
    fireEvent.click(duplicates[1]!);
    expect(last).toEqual({
      type: "OR",
      args: [
        { type: "EQUALS", field: "a", value: "x" },
        { type: "EQUALS", field: "b", value: "y" },
        { type: "EQUALS", field: "b", value: "y" }, // clone of index 1 appended at i+1
      ],
    });
  });

  it("duplicate group button appears on logic nodes when onDuplicate is provided", () => {
    const onDuplicate = vi.fn();
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ConditionNode
          value={{
            type: "AND",
            args: [
              { type: "EQUALS", field: "host", value: "srv-1" },
              { type: "EQUALS", field: "env", value: "prod" },
            ],
          }}
          fieldOptions={[]}
          onChange={() => undefined}
          onDuplicate={onDuplicate}
        />
      </Wrapper>,
    );
    const btn = screen.getByRole("button", { name: /duplicate group/i });
    expect(btn).toBeInTheDocument();
    fireEvent.click(btn);
    expect(onDuplicate).toHaveBeenCalledOnce();
  });

  it("duplicate button is absent for the child of a NOT node", () => {
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ConditionNode
          value={{
            type: "NOT",
            arg: { type: "EXISTS", field: "shelved" },
          }}
          fieldOptions={[]}
          onChange={() => undefined}
          isRoot
        />
      </Wrapper>,
    );
    expect(screen.queryByRole("button", { name: /duplicate/i })).not.toBeInTheDocument();
  });

  it("clone is structurally independent — mutating the clone does not affect the original", async () => {
    const user = userEvent.setup();
    let current: Condition = {
      type: "AND",
      args: [
        { type: "EQUALS", field: "host", value: "srv-1" },
        { type: "EQUALS", field: "env", value: "prod" },
      ],
    };
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ConditionNode value={current} fieldOptions={[]} onChange={(c) => (current = c)} isRoot />
      </Wrapper>,
    );
    const duplicates = screen.getAllByRole("button", { name: /^duplicate$/i });
    await user.click(duplicates[0]!);
    // current is now AND([host=srv-1, host=srv-1, env=prod])
    // Verify the second "host=srv-1" is a distinct object
    const andNode = current as { type: "AND"; args: Condition[] };
    expect(andNode.args[0]).not.toBe(andNode.args[1]);
  });
});
