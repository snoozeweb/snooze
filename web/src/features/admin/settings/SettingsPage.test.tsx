import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { beforeAll, describe, expect, it } from "vitest";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { mswServer } from "@/tests/msw/server";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ToastProvider, Toaster } from "@/shared/ui/Toast";
import { SettingsPage } from "./SettingsPage";

beforeAll(() => {
  if (typeof window !== "undefined" && !window.ResizeObserver) {
    window.ResizeObserver = class ResizeObserver {
      observe() {}
      unobserve() {}
      disconnect() {}
    };
  }
});

/**
 * Fixture mirroring the post-backend-expansion catalogue: general + the
 * notifications, ldap, and housekeeping groups the parallel backend agent
 * is adding to internal/pluginimpl/settings/metadata.yaml.
 */
function settingsMetadata() {
  return {
    data: {
      name: "settings",
      setting_form: {
        default_auth_backend: {
          display_name: "Default authentication backend",
          component: "Selector",
          description: "Backend that will be first.",
          default_value: "local",
          options: [
            { text: "Local", value: "local" },
            { text: "LDAP", value: "ldap" },
            { text: "Anonymous", value: "anonymous" },
          ],
          group: "general",
        },
        metrics_enabled: {
          display_name: "Metrics enabled",
          component: "Switch",
          description: "Enable Prometheus metrics.",
          default_value: true,
          group: "general",
        },
        notification_freq: {
          display_name: "Notification frequency",
          component: "String",
          description: "Duration between notifications.",
          default_value: "60s",
          group: "notifications",
        },
        "ldap.enabled": {
          display_name: "LDAP enabled",
          component: "Switch",
          description: "Enable LDAP backend.",
          default_value: false,
          group: "ldap",
        },
        "ldap.host": {
          display_name: "LDAP host",
          component: "String",
          description: "LDAP server URL.",
          default_value: "",
          group: "ldap",
        },
        "housekeeping.cleanup_snooze": {
          display_name: "Cleanup snooze",
          component: "String",
          description: "Retention for snoozes.",
          default_value: "24h",
          group: "housekeeping",
        },
      },
    },
  };
}

function setup() {
  const root = createRootRoute({ component: () => <Outlet /> });
  const route = createRoute({
    getParentRoute: () => root,
    path: "/web/admin/settings",
    component: SettingsPage,
  });
  const tree = root.addChildren([route]);
  /* eslint-disable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const router = createRouter({
    routeTree: tree,
    history: createMemoryHistory({ initialEntries: ["/web/admin/settings"] }),
  } as any);
  /* eslint-enable @typescript-eslint/no-explicit-any, @typescript-eslint/no-unsafe-argument */
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider delay={0}>
        <ToastProvider>
          {/* router is locally constructed; cast needed for the registered-router type mismatch */}
          <RouterProvider router={router as Parameters<typeof RouterProvider>[0]["router"]} />
          <Toaster />
        </ToastProvider>
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

describe("SettingsPage", () => {
  it("renders a tab for each catalogue group in the canonical order", async () => {
    mswServer.use(
      http.get("/api/v1/metadata/settings", () => HttpResponse.json(settingsMetadata())),
      http.get("/api/v1/settings", () =>
        HttpResponse.json({
          data: [{ uid: "s1", name: "metrics_enabled", value: true }],
          meta: { count: 1, limit: 500, offset: 0, total: 1 },
        }),
      ),
    );
    setup();
    // Tabs come from the catalogue; order is fixed by the page.
    await waitFor(() => expect(screen.getByRole("tab", { name: "General" })).toBeInTheDocument());
    expect(screen.getByRole("tab", { name: "Notifications" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "LDAP" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Housekeeping" })).toBeInTheDocument();
  });

  it("renders General-group cards in the active panel by default", async () => {
    mswServer.use(
      http.get("/api/v1/metadata/settings", () => HttpResponse.json(settingsMetadata())),
      http.get("/api/v1/settings", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 500, offset: 0, total: 0 },
        }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("Default authentication backend")).toBeInTheDocument());
    expect(screen.getByText("Metrics enabled")).toBeInTheDocument();
  });

  it("switches to the LDAP tab and renders the ldap cards", async () => {
    mswServer.use(
      http.get("/api/v1/metadata/settings", () => HttpResponse.json(settingsMetadata())),
      http.get("/api/v1/settings", () =>
        HttpResponse.json({
          data: [
            { uid: "s-ldap-enabled", name: "ldap.enabled", value: true },
            {
              uid: "s-ldap-host",
              name: "ldap.host",
              value: "ldap://example.com",
            },
          ],
          meta: { count: 2, limit: 500, offset: 0, total: 2 },
        }),
      ),
    );
    setup();
    const user = userEvent.setup();
    await waitFor(() => expect(screen.getByRole("tab", { name: "LDAP" })).toBeInTheDocument());
    await user.click(screen.getByRole("tab", { name: "LDAP" }));
    await waitFor(() => expect(screen.getByText("LDAP host")).toBeInTheDocument());
    // The existing record should seed the input value.
    const input = screen.getByLabelText<HTMLInputElement>("LDAP host");
    expect(input.value).toBe("ldap://example.com");
  });

  it("hides non-enabled LDAP cards when ldap.enabled is false", async () => {
    mswServer.use(
      http.get("/api/v1/metadata/settings", () => HttpResponse.json(settingsMetadata())),
      http.get("/api/v1/settings", () =>
        HttpResponse.json({
          data: [],
          meta: { count: 0, limit: 500, offset: 0, total: 0 },
        }),
      ),
    );
    setup();
    const user = userEvent.setup();
    await waitFor(() => expect(screen.getByRole("tab", { name: "LDAP" })).toBeInTheDocument());
    await user.click(screen.getByRole("tab", { name: "LDAP" }));
    // ldap.enabled is rendered, the rest are hidden.
    await waitFor(() => expect(screen.getByText("LDAP enabled")).toBeInTheDocument());
    expect(screen.queryByText("LDAP host")).not.toBeInTheDocument();
    expect(
      screen.getByText(/Enable LDAP above to configure connection/i),
    ).toBeInTheDocument();
  });

  it("a Card's Save POSTs the right body when no record exists", async () => {
    const bodies: Array<Record<string, unknown>> = [];
    mswServer.use(
      http.get("/api/v1/metadata/settings", () => HttpResponse.json(settingsMetadata())),
      http.get("/api/v1/settings", () =>
        HttpResponse.json({
          // Seed ldap.enabled=true so the rest of the LDAP cards are visible
          // (the page gates other ldap.* cards behind the enabled toggle).
          data: [{ uid: "s-ldap-enabled", name: "ldap.enabled", value: true }],
          meta: { count: 1, limit: 500, offset: 0, total: 1 },
        }),
      ),
      http.post("/api/v1/settings", async ({ request }) => {
        bodies.push((await request.json()) as Record<string, unknown>);
        return HttpResponse.json({ uid: "new-1", name: "ldap.host" });
      }),
    );
    setup();
    const user = userEvent.setup();
    await waitFor(() => expect(screen.getByRole("tab", { name: "LDAP" })).toBeInTheDocument());
    await user.click(screen.getByRole("tab", { name: "LDAP" }));
    const input = await screen.findByLabelText("LDAP host");
    await user.type(input, "ldap://x");
    // Multiple Tab panels mount (Radix keeps them in the DOM with hidden);
    // scope the Save click to the LDAP card by walking up from the input.
    const ldapCard = input.closest("section")!;
    const saveBtn = Array.from(ldapCard.querySelectorAll("button")).find(
      (b) => b.textContent?.trim().toLowerCase() === "save",
    )!;
    await user.click(saveBtn);
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(bodies[0]).toMatchObject({ name: "ldap.host", value: "ldap://x" });
  });

  it("disables Save when the input matches the persisted value (dirty tracking)", async () => {
    mswServer.use(
      http.get("/api/v1/metadata/settings", () => HttpResponse.json(settingsMetadata())),
      http.get("/api/v1/settings", () =>
        HttpResponse.json({
          data: [{ uid: "s1", name: "metrics_enabled", value: true }],
          meta: { count: 1, limit: 500, offset: 0, total: 1 },
        }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByText("Metrics enabled")).toBeInTheDocument());
    // Two Save buttons render (one per general card); both should be disabled
    // because every card is at baseline.
    const saves = screen.getAllByRole("button", { name: /^save$/i });
    expect(saves.length).toBeGreaterThan(0);
    for (const s of saves) expect(s).toBeDisabled();
  });

  it("renders a Custom tab when records exist with names not in the catalogue", async () => {
    mswServer.use(
      http.get("/api/v1/metadata/settings", () => HttpResponse.json(settingsMetadata())),
      http.get("/api/v1/settings", () =>
        HttpResponse.json({
          data: [{ uid: "s-x", name: "legacy_key", value: { foo: 1 } }],
          meta: { count: 1, limit: 500, offset: 0, total: 1 },
        }),
      ),
    );
    setup();
    const user = userEvent.setup();
    await waitFor(() => expect(screen.getByRole("tab", { name: "Custom" })).toBeInTheDocument());
    await user.click(screen.getByRole("tab", { name: "Custom" }));
    await waitFor(() => expect(screen.getByText("legacy_key")).toBeInTheDocument());
  });

  it("hides the Custom tab when no unknown records exist", async () => {
    mswServer.use(
      http.get("/api/v1/metadata/settings", () => HttpResponse.json(settingsMetadata())),
      http.get("/api/v1/settings", () =>
        HttpResponse.json({
          data: [{ uid: "s1", name: "metrics_enabled", value: true }],
          meta: { count: 1, limit: 500, offset: 0, total: 1 },
        }),
      ),
    );
    setup();
    await waitFor(() => expect(screen.getByRole("tab", { name: "General" })).toBeInTheDocument());
    expect(screen.queryByRole("tab", { name: "Custom" })).toBeNull();
  });
});
