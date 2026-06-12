import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { beforeAll, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ToastProvider, Toaster } from "@/shared/ui/Toast";
import { RuleEditor } from "./RuleEditor";

// Spy on the (expensive) deep-sort + YAML stringify so we can assert the
// diff section never runs it while collapsed — the crux of the perf fix.
const yamlSpy = vi.hoisted(() => vi.fn((obj: unknown) => JSON.stringify(obj)));
vi.mock("@/lib/yaml", () => ({ stableYaml: yamlSpy }));

// Radix UI's BubbleInput (used by Select) calls ResizeObserver in jsdom.
beforeAll(() => {
  if (typeof window !== "undefined" && !window.ResizeObserver) {
    window.ResizeObserver = class ResizeObserver {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
});

function wrap() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>
      <TooltipProvider delay={0}>
        <ToastProvider>
          {children}
          <Toaster />
        </ToastProvider>
      </TooltipProvider>
    </QueryClientProvider>
  );
}

describe("RuleEditor", () => {
  it("loads an existing rule into the form", async () => {
    mswServer.use(
      http.get("/api/v1/rule/rl1", () =>
        HttpResponse.json({
          uid: "rl1",
          name: "Tag prod",
          comment: "for noisy hosts",
          enabled: true,
          condition: { type: "EQUALS", field: "host", value: "srv-1" },
          modifications: [["SET", "environment", "prod"]],
        }),
      ),
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [{ host: "srv-1" }],
          meta: { count: 1, limit: 50, offset: 0, total: 1 },
        }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <RuleEditor plugin="rule" uid="rl1" onClose={() => undefined} />
      </Wrapper>,
    );
    await waitFor(() => expect(screen.getByDisplayValue("Tag prod")).toBeInTheDocument());
    expect(screen.getByDisplayValue("for noisy hosts")).toBeInTheDocument();
    expect(screen.getByDisplayValue("environment")).toBeInTheDocument();
  });

  it("creates a new rule on Save", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/rule", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ uid: "rl-new", name: "New" });
      }),
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 50, offset: 0, total: 0 },
        }),
      ),
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <RuleEditor plugin="rule" uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    await user.type(screen.getByLabelText(/name/i), "Tag prod hosts");
    await user.click(screen.getByRole("button", { name: /create/i }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect((bodies[0] as { name: string }).name).toBe("Tag prod hosts");
    expect(onClose).toHaveBeenCalled();
  });

  it("updates an existing rule on Save", async () => {
    const patches: unknown[] = [];
    mswServer.use(
      http.get("/api/v1/rule/rl1", () =>
        HttpResponse.json({ uid: "rl1", name: "Old name", enabled: true }),
      ),
      http.patch("/api/v1/rule/rl1", async ({ request }) => {
        patches.push(await request.json());
        return HttpResponse.json({ uid: "rl1", name: "New name" });
      }),
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 50, offset: 0, total: 0 },
        }),
      ),
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <RuleEditor plugin="rule" uid="rl1" onClose={onClose} />
      </Wrapper>,
    );
    await waitFor(() => expect(screen.getByDisplayValue("Old name")).toBeInTheDocument());
    const nameInput = screen.getByDisplayValue("Old name");
    await user.clear(nameInput);
    await user.type(nameInput, "New name");
    await user.click(screen.getByRole("button", { name: /save/i }));
    await waitFor(() => expect(patches).toHaveLength(1));
    expect((patches[0] as { name: string }).name).toBe("New name");
    expect(onClose).toHaveBeenCalled();
  });

  it("shows the Diff section in edit mode", async () => {
    mswServer.use(
      http.get("/api/v1/rule/rl1", () =>
        HttpResponse.json({
          uid: "rl1",
          name: "Tag prod",
          enabled: true,
          condition: { type: "ALWAYS_TRUE" },
        }),
      ),
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 50, offset: 0, total: 0 },
        }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <RuleEditor plugin="rule" uid="rl1" onClose={() => undefined} />
      </Wrapper>,
    );
    expect(await screen.findByRole("button", { name: /^diff/i })).toBeInTheDocument();
  });

  it("does not run stableYaml while typing in Name with the diff collapsed", async () => {
    mswServer.use(
      http.get("/api/v1/rule/rl1", () =>
        HttpResponse.json({
          uid: "rl1",
          name: "Old name",
          enabled: true,
          condition: { type: "ALWAYS_TRUE" },
        }),
      ),
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 50, offset: 0, total: 0 },
        }),
      ),
    );
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <RuleEditor plugin="rule" uid="rl1" onClose={() => undefined} />
      </Wrapper>,
    );
    const nameInput = await screen.findByDisplayValue("Old name");
    yamlSpy.mockClear();
    // Diff is collapsed by default; typing the Name must not recompute YAML.
    await user.type(nameInput, " edited");
    expect(yamlSpy).not.toHaveBeenCalled();
    // Expanding the diff computes both sides (old + new) exactly once.
    await user.click(screen.getByRole("button", { name: /^diff/i }));
    await waitFor(() => expect(yamlSpy.mock.calls.length).toBeGreaterThan(0));
    expect(screen.getByLabelText(/^Diff$/)).toBeInTheDocument();
  });

  it("hides the Diff section when creating", () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 50, offset: 0, total: 0 },
        }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <RuleEditor plugin="rule" uid={undefined} onClose={() => undefined} />
      </Wrapper>,
    );
    expect(screen.queryByRole("button", { name: /^diff/i })).not.toBeInTheDocument();
  });
});
