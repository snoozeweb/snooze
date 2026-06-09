import { describe, expect, it, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";
import { authStore } from "@/lib/auth/store";

const navigate = vi.fn();
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigate,
}));

import { LoginCallback } from "./LoginCallback";

describe("LoginCallback", () => {
  beforeEach(() => {
    navigate.mockReset();
    vi.spyOn(authStore.getState(), "login").mockImplementation(() => {});
  });

  it("stores the token from the fragment and navigates to return_to", async () => {
    window.location.hash = "#token=jwt123&refresh_token=rt456&return_to=%2Fweb%2Frules";
    render(<LoginCallback />);
    await vi.waitFor(() => {
      expect(authStore.getState().login).toHaveBeenCalledWith("jwt123", "rt456");
      expect(navigate).toHaveBeenCalledWith({ to: "/web/rules" });
    });
  });

  it("redirects to /web/login when no token is present", async () => {
    window.location.hash = "#oops=1";
    render(<LoginCallback />);
    await vi.waitFor(() => {
      expect(navigate).toHaveBeenCalledWith({ to: "/web/login" });
    });
  });
});
