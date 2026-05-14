import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ToastProvider } from "@/shared/ui/Toast";
import { PrimitivesPage } from "./PrimitivesPage";

describe("PrimitivesPage", () => {
  it("renders the Primitives heading", () => {
    render(
      <TooltipProvider>
        <ToastProvider>
          <PrimitivesPage />
        </ToastProvider>
      </TooltipProvider>,
    );
    expect(screen.getByRole("heading", { level: 1, name: "Primitives" })).toBeInTheDocument();
  });
});
