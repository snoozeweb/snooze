import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { buildTree, parentKey, sortSiblings } from "./tree";
import { RulesTreeTable } from "./RulesTreeTable";
import type { Rule } from "./types";

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

// ---- pure tree-building helpers ---------------------------------------------

describe("sortSiblings", () => {
  it("orders by tree_order ascending, name as tiebreak", () => {
    const out = sortSiblings([
      { name: "c", tree_order: 0 },
      { name: "a", tree_order: 2 },
      { name: "b", tree_order: 1 },
    ]);
    expect(out.map((r) => r.name)).toEqual(["c", "b", "a"]);
  });

  it("places rules without tree_order at the end, ordered by name", () => {
    const out = sortSiblings([
      { name: "zulu" },
      { name: "alpha", tree_order: 0 },
      { name: "bravo" },
    ]);
    expect(out.map((r) => r.name)).toEqual(["alpha", "bravo", "zulu"]);
  });
});

describe("parentKey", () => {
  it("returns __root__ for rules with no parents", () => {
    expect(parentKey({ name: "x" })).toBe("__root__");
    expect(parentKey({ name: "x", parents: [] })).toBe("__root__");
  });
  it("returns the first parent uid when present", () => {
    expect(parentKey({ name: "x", parents: ["p1"] })).toBe("p1");
    expect(parentKey({ name: "x", parents: ["p1", "p2"] })).toBe("p1");
  });
});

describe("buildTree", () => {
  it("groups children under their parent and assigns ascending depth", () => {
    const rules: Rule[] = [
      { uid: "r1", name: "root-a", tree_order: 0 },
      { uid: "r2", name: "child-of-a", parents: ["r1"], tree_order: 0 },
      { uid: "r3", name: "root-b", tree_order: 1 },
      { uid: "r4", name: "grandchild", parents: ["r2"], tree_order: 0 },
    ];
    const { roots } = buildTree(rules);
    expect(roots.map((n) => n.rule.name)).toEqual(["root-a", "root-b"]);
    expect(roots[0]!.children.map((n) => n.rule.name)).toEqual(["child-of-a"]);
    expect(roots[0]!.children[0]!.children.map((n) => n.rule.name)).toEqual(["grandchild"]);
    expect(roots[0]!.depth).toBe(0);
    expect(roots[0]!.children[0]!.depth).toBe(1);
    expect(roots[0]!.children[0]!.children[0]!.depth).toBe(2);
  });

  it("promotes orphans (parent uid not in set) to root level", () => {
    // A rule referencing a parent that was filtered out / deleted shouldn't
    // disappear from the UI — it surfaces at the root instead.
    const rules: Rule[] = [
      { uid: "r1", name: "orphan", parents: ["missing"] },
      { uid: "r2", name: "alive" },
    ];
    const { roots } = buildTree(rules);
    expect(roots.map((n) => n.rule.name).sort()).toEqual(["alive", "orphan"]);
  });
});

// ---- component rendering ---------------------------------------------------

describe("RulesTreeTable", () => {
  it("renders the empty state when there are no rules", () => {
    const Wrapper = wrap();
    render(
      <Wrapper>
        <RulesTreeTable rules={[]} onRowOpen={() => undefined} />
      </Wrapper>,
    );
    expect(screen.getByText("No rules yet.")).toBeInTheDocument();
  });

  it("renders rows in tree_order and shows children indented", () => {
    const rules: Rule[] = [
      { uid: "r1", name: "second", tree_order: 1 },
      { uid: "r2", name: "first", tree_order: 0 },
      { uid: "r3", name: "child-of-first", parents: ["r2"], tree_order: 0 },
    ];
    const Wrapper = wrap();
    render(
      <Wrapper>
        <RulesTreeTable rules={rules} onRowOpen={() => undefined} />
      </Wrapper>,
    );
    // The first data row is "first" (tree_order 0), second row is its child,
    // and third row is "second" (tree_order 1).
    const rows = screen.getAllByRole("row");
    expect(rows[0]).toHaveTextContent("first");
    expect(rows[1]).toHaveTextContent("child-of-first");
    expect(rows[2]).toHaveTextContent("second");
  });

  it("clicking the row body (not the handle) calls onRowOpen with the rule", () => {
    const onRowOpen = vi.fn();
    const rules: Rule[] = [{ uid: "r1", name: "alpha" }];
    const Wrapper = wrap();
    render(
      <Wrapper>
        <RulesTreeTable rules={rules} onRowOpen={onRowOpen} />
      </Wrapper>,
    );
    fireEvent.click(screen.getByText("alpha"));
    expect(onRowOpen).toHaveBeenCalledWith(rules[0]);
  });

  it("clicking the drag handle does not open the editor", () => {
    const onRowOpen = vi.fn();
    const rules: Rule[] = [{ uid: "r1", name: "alpha" }];
    const Wrapper = wrap();
    render(
      <Wrapper>
        <RulesTreeTable rules={rules} onRowOpen={onRowOpen} />
      </Wrapper>,
    );
    const handle = screen.getByLabelText("Drag alpha");
    fireEvent.click(handle);
    expect(onRowOpen).not.toHaveBeenCalled();
  });

  it("renders an expand chevron on each row", () => {
    const rules: Rule[] = [{ uid: "r1", name: "alpha" }];
    const Wrapper = wrap();
    render(
      <Wrapper>
        <RulesTreeTable rules={rules} onRowOpen={() => undefined} />
      </Wrapper>,
    );
    expect(screen.getByLabelText("Expand row alpha")).toBeInTheDocument();
  });

  it("clicking the expand chevron toggles the details panel without opening the editor", () => {
    const onRowOpen = vi.fn();
    const rules: Rule[] = [{ uid: "r1", name: "alpha" }];
    const Wrapper = wrap();
    render(
      <Wrapper>
        <RulesTreeTable rules={rules} onRowOpen={onRowOpen} />
      </Wrapper>,
    );
    const chevron = screen.getByLabelText("Expand row alpha");
    expect(chevron).toHaveAttribute("aria-expanded", "false");
    fireEvent.click(chevron);
    expect(chevron).toHaveAttribute("aria-expanded", "true");
    // Details panel mounts: AuditTimeline heading + JsonViewer content (rule uid).
    expect(screen.getByText("Audit log")).toBeInTheDocument();
    expect(onRowOpen).not.toHaveBeenCalled();
    // Toggle back closes it.
    fireEvent.click(chevron);
    expect(chevron).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("Audit log")).not.toBeInTheDocument();
  });

  it("expand chevrons on sibling rows are independent", () => {
    const rules: Rule[] = [
      { uid: "r1", name: "alpha", tree_order: 0 },
      { uid: "r2", name: "bravo", tree_order: 1 },
    ];
    const Wrapper = wrap();
    render(
      <Wrapper>
        <RulesTreeTable rules={rules} onRowOpen={() => undefined} />
      </Wrapper>,
    );
    const alphaChevron = screen.getByLabelText("Expand row alpha");
    fireEvent.click(alphaChevron);
    expect(alphaChevron).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByLabelText("Expand row bravo")).toHaveAttribute("aria-expanded", "false");
  });

  it("the drag handle still works (does not open editor) after expanding details", () => {
    const onRowOpen = vi.fn();
    const rules: Rule[] = [{ uid: "r1", name: "alpha" }];
    const Wrapper = wrap();
    render(
      <Wrapper>
        <RulesTreeTable rules={rules} onRowOpen={onRowOpen} />
      </Wrapper>,
    );
    fireEvent.click(screen.getByLabelText("Expand row alpha"));
    fireEvent.click(screen.getByLabelText("Drag alpha"));
    expect(onRowOpen).not.toHaveBeenCalled();
  });
});
