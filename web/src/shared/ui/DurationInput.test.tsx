import { render, screen, fireEvent } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { DurationInput } from "./DurationInput";

describe("DurationInput", () => {
  it("renders a human-readable badge next to the seconds input", () => {
    render(<DurationInput value={3600} onChange={() => undefined} aria-label="TTL" />);
    // 3600s == "1h" — secondsToHuman()
    expect(screen.getByText("1h")).toBeInTheDocument();
    expect((screen.getByLabelText("TTL") as HTMLInputElement).value).toBe("3600");
  });
  it("emits the new number when the user types", () => {
    const onChange = vi.fn();
    render(<DurationInput value={0} onChange={onChange} aria-label="TTL" />);
    fireEvent.change(screen.getByLabelText("TTL"), { target: { value: "7200" } });
    expect(onChange).toHaveBeenCalledWith(7200);
  });
  it("renders 'forever' when value is 0", () => {
    render(<DurationInput value={0} onChange={() => undefined} aria-label="TTL" />);
    expect(screen.getByText("forever")).toBeInTheDocument();
  });
});
