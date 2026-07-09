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

// The member picker reads the existing user list. Tests that add a member stub
// it with a known set so the dropdown offers predictable options.
function stubUsers(users: { name: string; method: string }[]) {
  mswServer.use(
    http.get("/api/v1/user", () =>
      HttpResponse.json({
        data: users.map((u, i) => ({ uid: `u${i}`, ...u })),
        meta: { count: users.length, limit: 500, offset: 0, total: users.length },
      }),
    ),
  );
}

describe("GroupEditor", () => {
  it("renders name, description, and the member picker", () => {
    const onClose = vi.fn();
    const Wrapper = wrap();
    render(
      <Wrapper>
        <GroupEditor uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    expect(screen.getByLabelText(/^name$/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/description/i)).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: /members/i })).toBeInTheDocument();
  });

  it("picks a member from the existing-users dropdown, showing its auth method", async () => {
    stubUsers([{ name: "alice", method: "local" }]);
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <GroupEditor uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    await user.click(screen.getByRole("combobox", { name: /members/i }));
    // The option carries the auth method as muted secondary text, so an operator
    // doesn't have to know or guess which backend owns the account.
    const option = await screen.findByRole("option", { name: /alice/ });
    expect(option).toHaveTextContent(/local/i);
    await user.click(option);
    // The chosen user renders as a removable pill inside the combobox.
    expect(screen.getByLabelText("Remove alice")).toBeInTheDocument();
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
    // Wait for form hydration — the member renders as a pill with a remove button.
    await waitFor(() => expect(screen.getByLabelText("Remove alice")).toBeInTheDocument());
    await user.click(screen.getByLabelText("Remove alice"));
    expect(screen.queryByLabelText("Remove alice")).not.toBeInTheDocument();
  });

  it("creates a group with name, description, and a picked member", async () => {
    stubUsers([{ name: "bob", method: "ldap" }]);
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
    await user.click(screen.getByRole("combobox", { name: /members/i }));
    await user.click(await screen.findByRole("option", { name: /bob/ }));
    await user.click(screen.getByRole("button", { name: /create/i }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    const sent = bodies[0] as {
      name: string;
      description: string;
      members: { username: string; method: string }[];
    };
    expect(sent.name).toBe("sre-test");
    expect(sent.description).toBe("Test group");
    // The picker preserves the user's real auth method — the admin never typed it.
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
