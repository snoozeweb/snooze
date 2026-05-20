import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import {
  buildTree,
  collectSubtreeIds,
  flattenTree,
  parentKey,
  projectDrop,
  ROOT,
  sortSiblings,
} from "./tree";
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

describe("flattenTree + collectSubtreeIds", () => {
  const rules: Rule[] = [
    { uid: "r1", name: "root-a", tree_order: 0 },
    { uid: "r2", name: "child-a", parents: ["r1"], tree_order: 0 },
    { uid: "r3", name: "grandchild", parents: ["r2"], tree_order: 0 },
    { uid: "r4", name: "root-b", tree_order: 1 },
  ];

  it("flattens to a depth-aware ordered list with parent links", () => {
    const flat = flattenTree(buildTree(rules).roots);
    expect(flat.map((n) => n.rule.name)).toEqual([
      "root-a",
      "child-a",
      "grandchild",
      "root-b",
    ]);
    expect(flat.map((n) => n.depth)).toEqual([0, 1, 2, 0]);
    expect(flat.map((n) => n.parentId)).toEqual([ROOT, "r1", "r2", ROOT]);
  });

  it("collects the full subtree rooted at a given uid", () => {
    const flat = flattenTree(buildTree(rules).roots);
    const sub = collectSubtreeIds(flat, "r1");
    expect([...sub].sort()).toEqual(["r1", "r2", "r3"]);
  });
});

describe("projectDrop", () => {
  it("snaps to the previous row's depth by default", () => {
    // [a (depth 0), b (depth 0)] — dropping at index 1 (between a and b)
    // with no horizontal drift lands flush with a at root depth.
    const flat = flattenTree(
      buildTree([
        { uid: "a", name: "a", tree_order: 0 },
        { uid: "b", name: "b", tree_order: 1 },
      ]).roots,
    );
    const out = projectDrop(flat, 1, 0);
    expect(out).toEqual({ parentId: ROOT, depth: 0 });
  });

  it("indenting past the threshold makes the dropped row a child of the previous row", () => {
    const flat = flattenTree(
      buildTree([
        { uid: "a", name: "a", tree_order: 0 },
        { uid: "b", name: "b", tree_order: 1 },
      ]).roots,
    );
    // 25px past the 20px indent threshold → become a child of "a".
    const out = projectDrop(flat, 1, 25);
    expect(out).toEqual({ parentId: "a", depth: 1 });
  });

  it("outdenting goes back toward root when the next row allows it", () => {
    const flat = flattenTree(
      buildTree([
        { uid: "a", name: "a", tree_order: 0 },
        { uid: "b", name: "b", parents: ["a"], tree_order: 0 },
        { uid: "c", name: "c", tree_order: 1 },
      ]).roots,
    );
    // Between b (depth 1) and c (depth 0). c sits at root depth — strong
    // leftward drag should land at root.
    const out = projectDrop(flat, 2, -40);
    expect(out.depth).toBe(0);
    expect(out.parentId).toBe(ROOT);
  });

  it("between siblings of a deeper parent, the drop stays at that depth", () => {
    // [a, b (child of a)] — dropping between them snaps to b's depth so
    // we can't accidentally rip b out of its parent.
    const flat = flattenTree(
      buildTree([
        { uid: "a", name: "a", tree_order: 0 },
        { uid: "b", name: "b", parents: ["a"], tree_order: 0 },
      ]).roots,
    );
    const out = projectDrop(flat, 1, 0);
    expect(out).toEqual({ parentId: "a", depth: 1 });
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
    // getAllByRole("row") returns the header row + every data row.
    // Skip the header (index 0).
    const rows = screen.getAllByRole("row").slice(1);
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

  it("selecting a parent row auto-selects every descendant", () => {
    const rules: Rule[] = [
      { uid: "p", name: "parent", tree_order: 0 },
      { uid: "c1", name: "child1", parents: ["p"], tree_order: 0 },
      { uid: "c2", name: "child2", parents: ["p"], tree_order: 1 },
      { uid: "g", name: "grand", parents: ["c1"], tree_order: 0 },
    ];
    const Wrapper = wrap();
    render(
      <Wrapper>
        <RulesTreeTable rules={rules} onRowOpen={() => undefined} />
      </Wrapper>,
    );
    const parentBox = screen.getByLabelText("Select rule parent");
    fireEvent.click(parentBox);
    expect(parentBox).toHaveAttribute("data-state", "checked");
    expect(screen.getByLabelText("Select rule child1")).toHaveAttribute("data-state", "checked");
    expect(screen.getByLabelText("Select rule child2")).toHaveAttribute("data-state", "checked");
    expect(screen.getByLabelText("Select rule grand")).toHaveAttribute("data-state", "checked");
  });

  it("selecting any row reveals a bulk Delete control in the toolbar", () => {
    const rules: Rule[] = [{ uid: "r1", name: "solo" }];
    const Wrapper = wrap();
    render(
      <Wrapper>
        <RulesTreeTable rules={rules} onRowOpen={() => undefined} />
      </Wrapper>,
    );
    fireEvent.click(screen.getByLabelText("Select rule solo"));
    expect(screen.getByRole("button", { name: /Delete \(1\)/ })).toBeInTheDocument();
  });
});
