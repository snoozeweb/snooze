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
import { HeartbeatEditor } from "./HeartbeatEditor";

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

describe("HeartbeatEditor", () => {
  it("renders_name_and_interval_inputs", () => {
    const Wrapper = wrap();
    render(
      <Wrapper>
        <HeartbeatEditor uid={undefined} onClose={() => undefined} />
      </Wrapper>,
    );
    expect(screen.getByLabelText(/^name$/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/interval/i)).toBeInTheDocument();
  });

  it("create_mode_shows_token_generation_note", () => {
    const Wrapper = wrap();
    render(
      <Wrapper>
        <HeartbeatEditor uid={undefined} onClose={() => undefined} />
      </Wrapper>,
    );
    // Users should learn up front that a ping URL + API key appear after saving.
    expect(screen.getByText(/generated on save/i)).toBeInTheDocument();
  });

  it("renders_grace_and_max_latency_inputs", () => {
    const Wrapper = wrap();
    render(
      <Wrapper>
        <HeartbeatEditor uid={undefined} onClose={() => undefined} />
      </Wrapper>,
    );
    expect(screen.getByLabelText(/grace/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/max latency/i)).toBeInTheDocument();
  });

  it("submit_create_calls_post", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/heartbeat", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ uid: "hb-new", name: "my-hb", interval: 60 });
      }),
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <HeartbeatEditor uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    // Clear the default interval value and type name
    const nameInput = screen.getByLabelText(/^name$/i);
    await user.clear(nameInput);
    await user.type(nameInput, "my-hb");
    const intervalInput = screen.getByLabelText(/interval/i);
    await user.clear(intervalInput);
    await user.type(intervalInput, "60");
    await user.click(screen.getByRole("button", { name: /create/i }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    const body = bodies[0] as { name: string; interval: number };
    expect(body.name).toBe("my-hb");
    expect(body.interval).toBe(60);
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
  });

  it("edit_mode_prefills_fields", async () => {
    mswServer.use(
      http.get("/api/v1/heartbeat/uid-1", () =>
        HttpResponse.json({
          uid: "uid-1",
          name: "existing",
          interval: 120,
          grace: 30,
          token: "tok",
          status: "ok",
        }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <HeartbeatEditor uid="uid-1" onClose={() => undefined} />
      </Wrapper>,
    );
    await waitFor(() =>
      expect(screen.getByLabelText<HTMLInputElement>(/^name$/i).value).toBe("existing"),
    );
    expect(screen.getByLabelText<HTMLInputElement>(/interval/i).value).toBe("120");
    expect(screen.getByLabelText<HTMLInputElement>(/grace/i).value).toBe("30");
  });

  it("token_and_ping_url_shown_in_edit_mode", async () => {
    mswServer.use(
      http.get("/api/v1/heartbeat/uid-1", () =>
        HttpResponse.json({
          uid: "uid-1",
          name: "existing",
          interval: 120,
          grace: 30,
          token: "tok",
          status: "ok",
        }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <HeartbeatEditor uid="uid-1" onClose={() => undefined} />
      </Wrapper>,
    );
    // Token CopyField
    await waitFor(() => expect(screen.getByLabelText<HTMLInputElement>("Token").value).toBe("tok"));
    // Ping URL CopyField contains heartbeat path
    const pingUrlInput = screen.getByLabelText<HTMLInputElement>("Ping URL");
    expect(pingUrlInput.value).toContain("/api/v1/webhook/heartbeat?name=existing&token=tok");
  });

  it("status_badge_shown_in_edit_mode", async () => {
    mswServer.use(
      http.get("/api/v1/heartbeat/uid-2", () =>
        HttpResponse.json({
          uid: "uid-2",
          name: "overdue-hb",
          interval: 60,
          token: "tok2",
          status: "overdue",
        }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <HeartbeatEditor uid="uid-2" onClose={() => undefined} />
      </Wrapper>,
    );
    await waitFor(() => expect(screen.getByLabelText("status: overdue")).toBeInTheDocument());
  });

  it("readOnly_fields_absent_from_submit_body", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/heartbeat", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ uid: "hb-new", name: "clean-hb", interval: 60 });
      }),
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <HeartbeatEditor uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    const nameInput = screen.getByLabelText(/^name$/i);
    await user.clear(nameInput);
    await user.type(nameInput, "clean-hb");
    const intervalInput = screen.getByLabelText(/interval/i);
    await user.clear(intervalInput);
    await user.type(intervalInput, "60");
    await user.click(screen.getByRole("button", { name: /create/i }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    const body = bodies[0] as Record<string, unknown>;
    expect(body).not.toHaveProperty("token");
    expect(body).not.toHaveProperty("status");
    expect(body).not.toHaveProperty("last_latency");
  });

  it("409_conflict_shows_drawer_error", async () => {
    mswServer.use(
      http.post("/api/v1/heartbeat", () =>
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
        <HeartbeatEditor uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    const nameInput = screen.getByLabelText(/^name$/i);
    await user.clear(nameInput);
    await user.type(nameInput, "dup");
    await user.click(screen.getByRole("button", { name: /create/i }));
    expect(await screen.findByText(/name already taken/i)).toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();
  });
});
