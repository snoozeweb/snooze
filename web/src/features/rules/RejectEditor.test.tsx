import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ToastProvider, Toaster } from "@/shared/ui/Toast";
import { toastStore } from "@/shared/ui/toast/useToast";
import { RejectEditor } from "./RejectEditor";

beforeAll(() => {
  if (typeof window !== "undefined" && !window.ResizeObserver) {
    window.ResizeObserver = class ResizeObserver {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
});

beforeEach(() => {
  toastStore.clear();
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

describe("RejectEditor", () => {
  it("renders the name input, an enabled toggle, and the shared condition editor", () => {
    mswServer.use(
      http.get("/api/v1/record", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <RejectEditor uid={undefined} onClose={() => undefined} />
      </Wrapper>,
    );
    expect(screen.getByLabelText(/^name$/i)).toBeInTheDocument();
    expect(screen.getByRole("switch", { name: /enabled/i })).toBeInTheDocument();
    // The shared ConditionEditor renders a "Condition" section heading.
    expect(screen.getByText(/condition/i)).toBeInTheDocument();
    // No time-constraint section (unlike SnoozeEditor).
    expect(screen.queryByText(/time constraints/i)).not.toBeInTheDocument();
  });

  it("creates a new reject rule on Save with {name, enabled, condition}", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/reject", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ uid: "rj-new", name: "x" });
      }),
      http.get("/api/v1/record", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
      ),
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <RejectEditor uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    await user.type(screen.getByLabelText(/^name$/i), "block-legacy");
    await user.click(screen.getByRole("button", { name: /create/i }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    const body = bodies[0] as { name: string; enabled: boolean; condition: unknown };
    expect(body.name).toBe("block-legacy");
    expect(body.enabled).toBe(true);
    expect(body.condition).toEqual({ type: "ALWAYS_TRUE" });
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
  });

  it("pre-fills the form when editing an existing rule", async () => {
    mswServer.use(
      http.get("/api/v1/reject/rj1", () =>
        HttpResponse.json({
          uid: "rj1",
          name: "block-legacy",
          enabled: false,
          condition: { type: "ALWAYS_TRUE" },
        }),
      ),
      http.get("/api/v1/record", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <RejectEditor uid="rj1" onClose={() => undefined} />
      </Wrapper>,
    );
    await waitFor(() =>
      expect(screen.getByLabelText<HTMLInputElement>(/^name$/i).value).toBe("block-legacy"),
    );
    expect(screen.getByRole("button", { name: /^save$/i })).toBeInTheDocument();
  });

  it("surfaces the 'name already taken' conflict on a 409", async () => {
    mswServer.use(
      http.post("/api/v1/reject", () =>
        HttpResponse.json(
          { error: { code: "conflict", message: "name already taken" } },
          { status: 409 },
        ),
      ),
      http.get("/api/v1/record", () =>
        HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
      ),
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <RejectEditor uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    await user.type(screen.getByLabelText(/^name$/i), "dup");
    await user.click(screen.getByRole("button", { name: /create/i }));
    expect(await screen.findByText(/name already taken/i)).toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();
  });
});
