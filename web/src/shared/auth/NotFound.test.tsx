import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { NotFound } from "./NotFound";

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
  useLocation: () => ({ pathname: "/web/does-not-exist" }),
}));

vi.mock("@/lib/auth/store", () => ({
  useAuth: () => ({ claims: null }),
}));

describe("NotFound", () => {
  it("names the dead path, hints at ⌘K, and offers a way back — not a bare 'Not Found'", () => {
    render(<NotFound />);
    expect(screen.getByText(/page not found/i)).toBeInTheDocument();
    expect(screen.getByText(/\/web\/does-not-exist/)).toBeInTheDocument();
    expect(screen.getByText(/⌘K/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /back to/i })).toBeInTheDocument();
  });
});
