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
import { ForwardEditor } from "./ForwardEditor";

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

const recordHandler = http.get("/api/v1/record", () =>
  HttpResponse.json({ data: [], meta: { count: 0, limit: 50, offset: 0, total: 0 } }),
);

describe("ForwardEditor", () => {
  it("renders all required controls in create mode", () => {
    mswServer.use(recordHandler);
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ForwardEditor uid={undefined} onClose={() => undefined} />
      </Wrapper>,
    );
    expect(screen.getByLabelText(/^name$/i)).toBeInTheDocument();
    expect(screen.getByRole("switch", { name: /enabled/i })).toBeInTheDocument();
    expect(screen.getByLabelText(/^endpoint$/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/event classes/i)).toBeInTheDocument();
    expect(screen.getByText(/condition/i)).toBeInTheDocument();
    // Advanced section is collapsed; auth type select is not visible initially
    expect(screen.queryByLabelText(/auth type/i)).not.toBeInTheDocument();
  });

  it("creates a destination with bearer auth on Save", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/forward", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ uid: "f-new", name: "peer-prod" });
      }),
      recordHandler,
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <ForwardEditor uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    await user.type(screen.getByLabelText(/^name$/i), "peer-prod");
    await user.type(screen.getByLabelText(/^endpoint$/i), "https://peer/api/v1/alerts");

    // Open the Advanced section
    await user.click(screen.getByRole("button", { name: /advanced/i }));

    // Select bearer auth type
    await user.click(screen.getByLabelText(/auth type/i));
    await user.click(screen.getByRole("option", { name: /bearer token/i }));

    // Fill in the token
    await waitFor(() => expect(screen.getByLabelText(/^token$/i)).toBeInTheDocument());
    await user.type(screen.getByLabelText(/^token$/i), "tok123");

    await user.click(screen.getByRole("button", { name: /create/i }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    const body = bodies[0] as {
      name: string;
      enabled: boolean;
      endpoint: string;
      event_classes: string[];
      auth: { type: string; token: string };
      condition: { type: string };
    };
    expect(body.name).toBe("peer-prod");
    expect(body.enabled).toBe(true);
    expect(body.endpoint).toBe("https://peer/api/v1/alerts");
    expect(body.event_classes).toEqual(["*"]);
    expect(body.auth).toEqual({ type: "bearer", token: "tok123" });
    expect(body.condition).toEqual({ type: "ALWAYS_TRUE" });
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
  });

  it("pre-fills the form when editing an existing destination", async () => {
    mswServer.use(
      http.get("/api/v1/forward/f1", () =>
        HttpResponse.json({
          uid: "f1",
          name: "peer-prod",
          enabled: false,
          endpoint: "https://peer/api/v1/alerts",
          auth: { type: "basic", username: "u", password: "p" },
          event_classes: ["*"],
          condition: { type: "ALWAYS_TRUE" },
        }),
      ),
      recordHandler,
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ForwardEditor uid="f1" onClose={() => undefined} />
      </Wrapper>,
    );
    await waitFor(() =>
      expect(screen.getByLabelText<HTMLInputElement>(/^name$/i).value).toBe("peer-prod"),
    );
    expect(screen.getByRole("switch", { name: /enabled/i })).toHaveAttribute(
      "aria-checked",
      "false",
    );
    expect(screen.getByRole("button", { name: /^save$/i })).toBeInTheDocument();

    // Open the Advanced section to check auth
    await userEvent.click(screen.getByRole("button", { name: /advanced/i }));
    await waitFor(() => expect(screen.getByLabelText(/auth type/i)).toBeInTheDocument());
  });

  it("surfaces the 'name already taken' conflict on a 409", async () => {
    mswServer.use(
      http.post("/api/v1/forward", () =>
        HttpResponse.json(
          { error: { code: "conflict", message: "name already taken" } },
          { status: 409 },
        ),
      ),
      recordHandler,
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <ForwardEditor uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    await user.type(screen.getByLabelText(/^name$/i), "dup");
    await user.click(screen.getByRole("button", { name: /create/i }));
    expect(await screen.findByText(/name already taken/i)).toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();
  });
});
