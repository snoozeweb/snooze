import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ToastProvider, Toaster } from "@/shared/ui/Toast";
import { UserEditor } from "./UserEditor";

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

describe("UserEditor", () => {
  it("creates a new user with password when creating", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/user", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ uid: "u-new", name: "alice" });
      }),
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <UserEditor uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    await user.type(screen.getByLabelText(/^name$/i), "alice");
    await user.type(screen.getByLabelText(/^password$/i), "s3cr3t");
    await user.click(screen.getByRole("button", { name: /create/i }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect((bodies[0] as { name: string }).name).toBe("alice");
    expect((bodies[0] as { password: string }).password).toBe("s3cr3t");
  });

  it("updates an existing user without the password field", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.get("/api/v1/user/u1", () =>
        HttpResponse.json({ uid: "u1", name: "alice", type: "local" }),
      ),
      http.patch("/api/v1/user/u1", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ uid: "u1", name: "alice-updated" });
      }),
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <UserEditor uid="u1" onClose={onClose} />
      </Wrapper>,
    );
    // Wait for the form to load with the existing data
    await waitFor(() =>
      expect(screen.getByLabelText<HTMLInputElement>(/^name$/i).value).toBe("alice"),
    );
    // Clear name and type a new one
    await user.clear(screen.getByLabelText(/^name$/i));
    await user.type(screen.getByLabelText(/^name$/i), "alice-updated");
    await user.click(screen.getByRole("button", { name: /save/i }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect((bodies[0] as { name: string }).name).toBe("alice-updated");
    expect((bodies[0] as Record<string, unknown>).password).toBeUndefined();
  });
});
