import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { beforeAll, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ToastProvider, Toaster } from "@/shared/ui/Toast";
import { WidgetEditor } from "./WidgetEditor";

// jsdom polyfill — Radix BubbleInput uses ResizeObserver inside Drawer.
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

describe("WidgetEditor", () => {
  it("lists Patlite and Other in the subtype dropdown", () => {
    const Wrapper = wrap();
    render(
      <Wrapper>
        <WidgetEditor uid={undefined} onClose={() => {}} />
      </Wrapper>,
    );
    const subtypeSelect = screen.getByLabelText(/widget type/i);
    expect(subtypeSelect.tagName).toBe("SELECT");
    const options = within(subtypeSelect).getAllByRole<HTMLOptionElement>("option");
    const values = options.map((o) => o.value);
    expect(values).toContain("patlite");
    // "Other (free-form)" represented by the empty-string value.
    expect(values).toContain("");
  });

  it("renders host/port inputs when Patlite is selected and posts a typed config", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/widget", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ uid: "w-new", name: "x" });
      }),
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <WidgetEditor uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    await user.type(screen.getByLabelText(/^name$/i), "patlite-floor1");
    await user.selectOptions(screen.getByLabelText(/widget type/i), "patlite");
    // Typed Patlite fields should be visible now.
    const host = screen.getByLabelText<HTMLInputElement>(/^host\b/i);
    const port = screen.getByLabelText<HTMLInputElement>(/^port\b/i);
    expect(host).toBeInTheDocument();
    expect(port).toBeInTheDocument();
    // Default value should be applied for port.
    expect(port.value).toBe("80");
    await user.type(host, "192.0.2.10");
    await user.click(screen.getByRole("button", { name: /create/i }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    const body = bodies[0] as {
      name: string;
      widget_type: string;
      config: Record<string, unknown>;
    };
    expect(body.name).toBe("patlite-floor1");
    expect(body.widget_type).toBe("patlite");
    expect(body.config).toEqual({ host: "192.0.2.10", port: 80 });
    expect(onClose).toHaveBeenCalled();
  });

  it("shows the JSON textarea when Other is selected and posts the parsed object", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/widget", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ uid: "w-new", name: "x" });
      }),
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <WidgetEditor uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    await user.type(screen.getByLabelText(/^name$/i), "custom-widget");
    // Default is already Other (empty value). Type a custom subtype.
    const customType = screen.getByLabelText(/custom type/i);
    await user.type(customType, "iframe");
    const jsonTextarea = document.querySelector<HTMLTextAreaElement>(
      'textarea[name="config_json"]',
    );
    expect(jsonTextarea).not.toBeNull();
    await user.clear(jsonTextarea!);
    await user.type(jsonTextarea!, '{{"url":"https://example.com"}');
    await user.click(screen.getByRole("button", { name: /create/i }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    const body = bodies[0] as {
      name: string;
      widget_type?: string;
      config: Record<string, unknown>;
    };
    expect(body.name).toBe("custom-widget");
    expect(body.widget_type).toBe("iframe");
    expect(body.config).toEqual({ url: "https://example.com" });
  });

  it("blocks submit when JSON config is invalid in Other mode", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/widget", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ uid: "w-new", name: "x" });
      }),
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <WidgetEditor uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    await user.type(screen.getByLabelText(/^name$/i), "custom-widget");
    const jsonTextarea = document.querySelector<HTMLTextAreaElement>(
      'textarea[name="config_json"]',
    )!;
    await user.clear(jsonTextarea);
    await user.type(jsonTextarea, "not valid json");
    await user.click(screen.getByRole("button", { name: /create/i }));
    await waitFor(() =>
      expect(
        screen.getByText(/invalid json|expected a json object|unexpected token/i),
      ).toBeInTheDocument(),
    );
    expect(bodies).toHaveLength(0);
    expect(onClose).not.toHaveBeenCalled();
  });

  it("opens an existing unknown-type widget in Other mode with legacy JSON editable", async () => {
    mswServer.use(
      http.get("/api/v1/widget/w-legacy", () =>
        HttpResponse.json({
          uid: "w-legacy",
          name: "legacy",
          widget_type: "iframe",
          config: { url: "https://old.example" },
        }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <WidgetEditor uid="w-legacy" onClose={() => {}} />
      </Wrapper>,
    );
    await waitFor(() => {
      const sel = screen.getByLabelText<HTMLSelectElement>(/widget type/i);
      // Other is represented by empty-value option; the legacy widget_type is
      // surfaced via the custom-type text input.
      expect(sel.value).toBe("");
    });
    const customType = screen.getByLabelText<HTMLInputElement>(/custom type/i);
    expect(customType.value).toBe("iframe");
    const jsonTextarea = document.querySelector<HTMLTextAreaElement>(
      'textarea[name="config_json"]',
    )!;
    expect(jsonTextarea.value).toContain("https://old.example");
  });

  it("opens an existing patlite widget pre-populated with typed fields", async () => {
    mswServer.use(
      http.get("/api/v1/widget/w-pat", () =>
        HttpResponse.json({
          uid: "w-pat",
          name: "patlite-floor1",
          widget_type: "patlite",
          config: { host: "10.0.0.1", port: 8080 },
        }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <WidgetEditor uid="w-pat" onClose={() => {}} />
      </Wrapper>,
    );
    await waitFor(() => {
      const sel = screen.getByLabelText<HTMLSelectElement>(/widget type/i);
      expect(sel.value).toBe("patlite");
    });
    const host = screen.getByLabelText<HTMLInputElement>(/^host\b/i);
    const port = screen.getByLabelText<HTMLInputElement>(/^port\b/i);
    expect(host.value).toBe("10.0.0.1");
    expect(port.value).toBe("8080");
  });
});
