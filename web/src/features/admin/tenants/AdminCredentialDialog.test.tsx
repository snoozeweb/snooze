import { render, screen } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { AdminCredentialDialog } from "./AdminCredentialDialog";

describe("AdminCredentialDialog", () => {
  it("renders the username and password once", () => {
    render(
      <AdminCredentialDialog
        credential={{ username: "ops", password: "SECRET-PW-123", method: "local", created: true }}
        onClose={vi.fn()}
      />,
    );
    expect(screen.getByText("ops")).toBeInTheDocument();
    expect(screen.getByText("SECRET-PW-123")).toBeInTheDocument();
    expect(screen.getByText(/won't see/i)).toBeInTheDocument();
  });

  it("renders nothing when credential is null", () => {
    const { container } = render(<AdminCredentialDialog credential={null} onClose={vi.fn()} />);
    expect(container).toBeEmptyDOMElement();
  });
});
