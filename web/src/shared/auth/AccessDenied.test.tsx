import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AccessDenied } from "./AccessDenied";

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
}));

describe("AccessDenied", () => {
  it("names the missing permission and offers a way to a page the user can see", () => {
    render(
      <AccessDenied perms={["ro_user", "rw_user"]} homeTo="/web/dashboard" homeLabel="Dashboard" />,
    );
    expect(screen.getByText(/access denied/i)).toBeInTheDocument();
    expect(screen.getByText(/ro_user or rw_user/i)).toBeInTheDocument();
    // The escape CTA points at the caller-supplied permitted destination, not a
    // hardcoded (possibly also-denied) page.
    expect(screen.getByRole("button", { name: /go to dashboard/i })).toBeInTheDocument();
  });

  it("falls back to a generic message and Alerts when no props are given", () => {
    render(<AccessDenied />);
    expect(screen.getByText(/don't have permission/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /go to alerts/i })).toBeInTheDocument();
  });
});
