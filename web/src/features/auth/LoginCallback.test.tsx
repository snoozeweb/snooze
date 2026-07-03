import { describe, expect, it, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { authStore } from "@/lib/auth/store";

const navigate = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigate,
}));

import { LoginCallback } from "./LoginCallback";

// LoginCallback renders <Logo />, which calls useConsoleConfig() (a TanStack
// Query hook), so a QueryClientProvider must wrap the tree.
function renderCallback() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <LoginCallback />
    </QueryClientProvider>,
  );
}

describe("LoginCallback", () => {
  beforeEach(() => {
    navigate.mockReset();
    vi.spyOn(authStore.getState(), "login").mockImplementation(() => {});
  });

  it("stores the token from the fragment and navigates to return_to", async () => {
    window.location.hash = "#token=jwt123&refresh_token=rt456&return_to=%2Fweb%2Frules";
    renderCallback();
    await vi.waitFor(() => {
      expect(authStore.getState().login).toHaveBeenCalledWith("jwt123", "rt456");
      expect(navigate).toHaveBeenCalledWith({ to: "/web/rules" });
    });
  });

  it("preserves a deep link's encoded query state (no double-decode)", async () => {
    // The server single-encodes the destination into the fragment;
    // URLSearchParams decodes it once. A second decodeURIComponent would mangle
    // the still-encoded query state ('%3D' → '=').
    const dest = "/web/alerts?q=a%3Db";
    window.location.hash = `#token=jwt&return_to=${encodeURIComponent(dest)}`;
    renderCallback();
    await vi.waitFor(() => {
      expect(navigate).toHaveBeenCalledWith({ to: dest });
    });
  });

  it("keeps a destination containing a raw '%' instead of silently dropping it", async () => {
    // A second decodeURIComponent throws on a lone '%', which the old code
    // swallowed and fell back to /web/alerts.
    const dest = "/web/x?p=100%";
    window.location.hash = `#token=jwt&return_to=${encodeURIComponent(dest)}`;
    renderCallback();
    await vi.waitFor(() => {
      expect(navigate).toHaveBeenCalledWith({ to: dest });
    });
  });

  it("rejects a protocol-relative return_to (open-redirect guard)", async () => {
    window.location.hash = `#token=jwt&return_to=${encodeURIComponent("//evil.example")}`;
    renderCallback();
    await vi.waitFor(() => expect(navigate).toHaveBeenCalled());
    // Falls back to a safe same-origin path (the user's first permitted page),
    // never the attacker's cross-origin target.
    const dest = (navigate.mock.calls[0]![0] as { to: string }).to;
    expect(dest.startsWith("/")).toBe(true);
    expect(dest).not.toContain("evil");
  });

  it("redirects to /web/login when no token is present", async () => {
    window.location.hash = "#oops=1";
    renderCallback();
    await vi.waitFor(() => {
      expect(navigate).toHaveBeenCalledWith({ to: "/web/login" });
    });
  });
});
