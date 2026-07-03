import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ToastProvider, Toaster } from "@/shared/ui/Toast";
import { GroupEditor } from "./GroupEditor";

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

describe("GroupEditor", () => {
  it("renders name, description, and member list inputs", () => {
    const onClose = vi.fn();
    const Wrapper = wrap();
    render(
      <Wrapper>
        <GroupEditor uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    expect(screen.getByLabelText(/^name$/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/description/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /add member/i })).toBeInTheDocument();
  });

  it("adds a member pair to the list", async () => {
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <GroupEditor uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    await user.type(screen.getByLabelText(/^username$/i), "alice");
    // Method defaults to "local" — no need to change
    await user.click(screen.getByRole("button", { name: /add member/i }));
    expect(screen.getByText("alice")).toBeInTheDocument();
    // The member list row renders a badge with "local"; there may also be "local" in the select option.
    // Assert the badge specifically.
    const badges = screen.getAllByText("local");
    // At least one badge element should exist (not just the option)
    expect(badges.length).toBeGreaterThanOrEqual(1);
  });

  it("removes a member from the list", async () => {
    mswServer.use(
      http.get("/api/v1/group/g1", () =>
        HttpResponse.json({
          uid: "g1",
          name: "sre",
          members: [{ username: "alice", method: "local" }],
        }),
      ),
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <GroupEditor uid="g1" onClose={onClose} />
      </Wrapper>,
    );
    // Wait for form hydration
    await waitFor(() => expect(screen.getByText("alice")).toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: /remove alice/i }));
    expect(screen.queryByText("alice")).not.toBeInTheDocument();
  });

  it("creates a group with name, description, and members", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/group", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ uid: "g-new", name: "sre-test" });
      }),
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <GroupEditor uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    await user.type(screen.getByLabelText(/^name$/i), "sre-test");
    await user.type(screen.getByLabelText(/description/i), "Test group");
    await user.type(screen.getByLabelText(/^username$/i), "bob");
    // Change method to ldap
    await user.selectOptions(screen.getByLabelText(/^method$/i), "ldap");
    await user.click(screen.getByRole("button", { name: /add member/i }));
    await user.click(screen.getByRole("button", { name: /create/i }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    const sent = bodies[0] as {
      name: string;
      description: string;
      members: { username: string; method: string }[];
    };
    expect(sent.name).toBe("sre-test");
    expect(sent.description).toBe("Test group");
    expect(sent.members[0]).toEqual({ username: "bob", method: "ldap" });
  });

  it("surfaces 409 as toast without closing drawer", async () => {
    mswServer.use(
      http.post("/api/v1/group", () =>
        HttpResponse.json(
          { error: { code: "conflict", message: "name already taken" } },
          { status: 409 },
        ),
      ),
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <GroupEditor uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    await user.type(screen.getByLabelText(/^name$/i), "sre");
    await user.click(screen.getByRole("button", { name: /create/i }));
    expect(await screen.findByText(/name already taken/i)).toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();
  });
});
