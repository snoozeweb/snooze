import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Logo } from "./Logo";

// Mock useTheme to control the current theme in tests
vi.mock("@/shared/hooks/useTheme", () => ({
  useTheme: vi.fn().mockReturnValue({ theme: "light", setTheme: vi.fn(), toggleTheme: vi.fn() }),
}));

// Mock useConsoleConfig to control the config in tests
vi.mock("@/features/config/api", () => ({
  useConsoleConfig: vi.fn().mockReturnValue({ data: { logo: "" } }),
}));

import { useTheme } from "@/shared/hooks/useTheme";
import { useConsoleConfig } from "@/features/config/api";

function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <Logo />
    </QueryClientProvider>,
  );
}

describe("Logo", () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it("falls back to the bundled light-theme PNG when logo is empty", () => {
    vi.mocked(useTheme).mockReturnValue({
      theme: "light",
      setTheme: vi.fn(),
      toggleTheme: vi.fn(),
    });
    vi.mocked(useConsoleConfig).mockReturnValue({ data: { logo: "" } } as ReturnType<
      typeof useConsoleConfig
    >);
    setup();
    const img = screen.getByRole("img", { name: /snooze/i });
    expect((img as HTMLImageElement).src).toMatch(/\/web\/logo\.png$/);
  });

  it("falls back to the bundled dark-theme PNG when logo is empty and theme is dark", () => {
    vi.mocked(useTheme).mockReturnValue({ theme: "dark", setTheme: vi.fn(), toggleTheme: vi.fn() });
    vi.mocked(useConsoleConfig).mockReturnValue({ data: { logo: "" } } as ReturnType<
      typeof useConsoleConfig
    >);
    setup();
    const img = screen.getByRole("img", { name: /snooze/i });
    expect((img as HTMLImageElement).src).toMatch(/\/web\/logo_white\.png$/);
  });

  it("renders the custom logo URL when logo is non-empty, regardless of theme", () => {
    vi.mocked(useTheme).mockReturnValue({
      theme: "light",
      setTheme: vi.fn(),
      toggleTheme: vi.fn(),
    });
    vi.mocked(useConsoleConfig).mockReturnValue({
      data: { logo: "https://example.com/custom.png" },
    } as ReturnType<typeof useConsoleConfig>);
    setup();
    const img = screen.getByRole("img", { name: /snooze/i });
    expect((img as HTMLImageElement).src).toBe("https://example.com/custom.png");
  });

  it("renders a data-URI logo when logo is a data URI", () => {
    vi.mocked(useTheme).mockReturnValue({ theme: "dark", setTheme: vi.fn(), toggleTheme: vi.fn() });
    vi.mocked(useConsoleConfig).mockReturnValue({
      data: { logo: "data:image/png;base64,abc" },
    } as ReturnType<typeof useConsoleConfig>);
    setup();
    const img = screen.getByRole("img", { name: /snooze/i });
    expect((img as HTMLImageElement).src).toBe("data:image/png;base64,abc");
  });
});
