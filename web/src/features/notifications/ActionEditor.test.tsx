import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { beforeAll, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ToastProvider, Toaster } from "@/shared/ui/Toast";
import { ActionEditor } from "./ActionEditor";

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

// A minimal /metadata payload covering the components we exercise in tests.
// Each entry carries both `name` (YAML display label) and `plugin_name`
// (registry key) — the same shape the backend handler now stamps. Note that
// "script" here uses the real-world mismatched name ("Run a script") to lock
// in that the editor matches on plugin_name, not name.
function metadataPayload() {
  return {
    data: [
      {
        plugin_name: "script",
        name: "Run a script",
        display_name: "Run a script",
        action_form: {
          command: {
            display_name: "Command",
            component: "Arguments",
            required: true,
          },
          timeout: {
            display_name: "Timeout",
            component: "Number",
            default_value: 10,
          },
        },
      },
      {
        plugin_name: "webhook",
        name: "Call a webhook",
        display_name: "Call a webhook",
        action_form: {
          url: {
            display_name: "URL",
            component: "String",
            required: true,
          },
          method: {
            display_name: "Method",
            component: "String",
            default_value: "POST",
          },
          headers: {
            display_name: "Headers",
            component: "Arguments",
            placeholder: ["header_name", "header_value"],
          },
        },
      },
      {
        plugin_name: "record",
        name: "record",
        display_name: "Record",
        // No action_form — should be filtered out of the subtype dropdown.
      },
      {
        plugin_name: "jira",
        name: "Create a JIRA issue",
        category: "ticketing",
        action_form: {
          project_key: { display_name: "Project Key", component: "String" },
        },
        daemon: { name: "snooze-jira", blurb: "Auto-close.", doc_url: "https://docs/jira#daemon" },
      },
    ],
  };
}

describe("ActionEditor", () => {
  it("creates a new action with form fields on Save", async () => {
    const bodies: unknown[] = [];
    mswServer.use(
      http.get("/api/v1/metadata", () => HttpResponse.json(metadataPayload())),
      http.get("/api/v1/metadata/webhook", () =>
        HttpResponse.json({ data: metadataPayload().data[1] }),
      ),
      http.post("/api/v1/action", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ uid: "a-new", name: "x" });
      }),
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <ActionEditor uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    // Step 1: pick the integration from the gallery (card label = metadata `name`).
    const card = await screen.findByRole("button", { name: /Call a webhook/ });
    await user.click(card);
    // Step 2: the config form appears.
    await user.type(screen.getByLabelText(/^name$/i), "hook-prod");
    const url = await screen.findByLabelText(/URL/);
    await user.type(url, "https://example.com");
    await user.click(screen.getByRole("button", { name: /create/i }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    const body = bodies[0] as {
      name: string;
      action: { selected: string; subcontent: Record<string, unknown> };
    };
    expect(body.name).toBe("hook-prod");
    expect(body.action.selected).toBe("webhook");
    expect(body.action.subcontent).toMatchObject({
      url: "https://example.com",
      method: "POST",
    });
    expect(onClose).toHaveBeenCalled();
  });

  it("shows only plugins that have an action_form as gallery cards", async () => {
    mswServer.use(http.get("/api/v1/metadata", () => HttpResponse.json(metadataPayload())));
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ActionEditor uid={undefined} onClose={() => undefined} />
      </Wrapper>,
    );
    // Cards are labelled by the metadata `name`.
    expect(await screen.findByRole("button", { name: /Run a script/ })).toBeTruthy();
    expect(screen.getByRole("button", { name: /Call a webhook/ })).toBeTruthy();
    // The record plugin has no action_form and must NOT appear as a card.
    expect(screen.queryByRole("button", { name: /^Record$/ })).toBeNull();
  });

  it("renders the typed form when the plugin's name differs from its plugin_name (registry key)", async () => {
    // Regression: most action plugins use the YAML `name:` field as a human
    // display label ("Send email", "Run a script") that doesn't equal the
    // Action's `action.selected` (the registry key). The editor must match on
    // plugin_name, not name, so the typed form renders for "mail" / "script"
    // / "webhook" instead of falling back to a JSON textarea.
    mswServer.use(
      http.get("/api/v1/metadata", () =>
        HttpResponse.json({
          data: [
            {
              plugin_name: "mail",
              name: "Send email", // YAML `name:` mismatch — human label.
              display_name: "Send email",
              action_form: {
                host: {
                  display_name: "Host",
                  component: "String",
                  default_value: "localhost",
                },
              },
            },
          ],
        }),
      ),
      http.get("/api/v1/action/a1", () =>
        HttpResponse.json({
          uid: "a1",
          name: "smtp-prod",
          // Wire shape: action.selected is the registry key (not the YAML
          // label) and the form payload lives under action.subcontent.
          action: { selected: "mail", subcontent: { host: "smtp.example.com" } },
        }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ActionEditor uid="a1" onClose={() => undefined} />
      </Wrapper>,
    );
    // The typed Host field appears when plugin matching succeeds; if we'd
    // matched on `name` instead of `plugin_name` the editor would fall back
    // to the action_json textarea and this label would never render.
    const host = await screen.findByLabelText(/host/i);
    expect((host as HTMLInputElement).value).toBe("smtp.example.com");
    // And the JSON fallback must NOT be present.
    expect(document.querySelector('textarea[name="subcontent_json"]')).toBeNull();
  });

  it("falls back to a JSON textarea when the selected plugin isn't in metadata", async () => {
    mswServer.use(
      http.get("/api/v1/metadata", () => HttpResponse.json(metadataPayload())),
      http.get("/api/v1/action/a1", () =>
        HttpResponse.json({
          uid: "a1",
          name: "legacy",
          action: { selected: "unknown-plugin", subcontent: { foo: "bar" } },
        }),
      ),
    );
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ActionEditor uid="a1" onClose={() => undefined} />
      </Wrapper>,
    );
    // The raw JSON textarea is the safety net for unknown plugins.
    const ta = await waitFor(() => {
      const el = document.querySelector<HTMLTextAreaElement>('textarea[name="subcontent_json"]');
      if (!el) throw new Error("not yet");
      return el;
    });
    expect(ta.value).toMatch(/"foo":\s*"bar"/);
  });

  it("shows the daemon chooser before the form for an integration with a daemon", async () => {
    mswServer.use(http.get("/api/v1/metadata", () => HttpResponse.json(metadataPayload())));
    const user = userEvent.setup();
    const Wrapper = wrap();
    render(
      <Wrapper>
        <ActionEditor uid={undefined} onClose={() => undefined} />
      </Wrapper>,
    );
    // Gallery loads; pick the jira plugin which has a daemon block.
    await user.click(await screen.findByRole("button", { name: /Create a JIRA issue/ }));
    // Chooser is shown, not the config form.
    expect(screen.getByText("Built-in")).toBeTruthy();
    expect(screen.queryByText("Project Key")).toBeNull();
    // Clicking Built-in advances to the config form.
    await user.click(screen.getByText("Built-in"));
    expect(await screen.findByText("Project Key")).toBeTruthy();
  });
});
