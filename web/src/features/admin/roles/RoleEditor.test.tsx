import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { mswServer } from "@/tests/msw/server";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ToastProvider, Toaster } from "@/shared/ui/Toast";
import { RoleEditor } from "./RoleEditor";

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

// All three RoleEditor tests stub the catalogue endpoint
// (GET /api/v1/permissions) explicitly so the editor sees a known set of
// options regardless of MSW's catch-all list handler.
const CATALOGUE = ["ro_all", "ro_record", "ro_rule", "rw_all", "rw_record", "rw_rule"];

function stubCatalogue() {
  mswServer.use(
    http.get("/api/v1/permissions", () =>
      HttpResponse.json({ data: CATALOGUE }),
    ),
  );
}

describe("RoleEditor", () => {
  it("renders a permissions combobox and adds a selected permission as a badge", async () => {
    stubCatalogue();
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <RoleEditor uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    // The combobox is exposed with an accessible name matching the field
    // label so screen reader users can find it the same way the
    // <label htmlFor=…> input pattern used to work for the textarea.
    const combobox = await screen.findByRole("combobox", { name: /permissions/i });
    expect(combobox).toBeInTheDocument();

    // Open the popover and pick a permission.
    await user.click(combobox);
    await user.click(await screen.findByRole("option", { name: /rw_rule/ }));

    // The selected permission renders as a removable badge inside the
    // combobox; the X button is labelled "Remove rw_rule".
    expect(screen.getByLabelText("Remove rw_rule")).toBeInTheDocument();
  });

  it("removes a permission when the badge X is clicked", async () => {
    stubCatalogue();
    mswServer.use(
      http.get("/api/v1/role/r1", () =>
        HttpResponse.json({
          uid: "r1",
          name: "analyst",
          permissions: ["rw_rule", "ro_record"],
        }),
      ),
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <RoleEditor uid="r1" onClose={onClose} />
      </Wrapper>,
    );
    // Wait for the existing role to hydrate the form.
    await waitFor(() =>
      expect(screen.getByLabelText<HTMLInputElement>(/^name$/i).value).toBe("analyst"),
    );
    expect(screen.getByLabelText("Remove rw_rule")).toBeInTheDocument();
    expect(screen.getByLabelText("Remove ro_record")).toBeInTheDocument();

    await user.click(screen.getByLabelText("Remove rw_rule"));
    expect(screen.queryByLabelText("Remove rw_rule")).not.toBeInTheDocument();
    // The other one remains.
    expect(screen.getByLabelText("Remove ro_record")).toBeInTheDocument();
  });

  it("submits the selected permissions as a string[] on Save", async () => {
    stubCatalogue();
    const bodies: unknown[] = [];
    mswServer.use(
      http.post("/api/v1/role", async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ uid: "r-new", name: "analyst" });
      }),
    );
    const onClose = vi.fn();
    const Wrapper = wrap();
    const user = userEvent.setup();
    render(
      <Wrapper>
        <RoleEditor uid={undefined} onClose={onClose} />
      </Wrapper>,
    );
    await user.type(screen.getByLabelText(/^name$/i), "analyst");
    const combobox = await screen.findByRole("combobox", { name: /permissions/i });
    await user.click(combobox);
    await user.click(await screen.findByRole("option", { name: /rw_rule/ }));
    // Reopen for the second selection (popover closes after a click on
    // some Radix versions; tapping the wrapper reopens it).
    await user.click(combobox);
    await user.click(await screen.findByRole("option", { name: /ro_rule/ }));

    await user.click(screen.getByRole("button", { name: /create/i }));
    await waitFor(() => expect(bodies).toHaveLength(1));
    const sent = bodies[0] as { permissions: string[] };
    expect(sent.permissions).toEqual(["rw_rule", "ro_rule"]);
  });
});
