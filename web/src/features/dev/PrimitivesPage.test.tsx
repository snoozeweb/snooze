import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import { ToastProvider } from "@/shared/ui/Toast";
import { PrimitivesPage } from "./PrimitivesPage";

describe("PrimitivesPage", () => {
  it("renders the Primitives heading", () => {
    // The Avatar gallery reads the people directory, hence the query client.
    render(
      <QueryClientProvider client={new QueryClient()}>
        <TooltipProvider>
          <ToastProvider>
            <PrimitivesPage />
          </ToastProvider>
        </TooltipProvider>
      </QueryClientProvider>,
    );
    expect(screen.getByRole("heading", { level: 1, name: "Primitives" })).toBeInTheDocument();
  });
});
