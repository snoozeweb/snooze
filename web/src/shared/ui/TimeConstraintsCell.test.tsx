import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { TimeConstraintsCell } from "./TimeConstraintsCell";
import { summarizeTimeConstraints } from "./TimeConstraintsEditor";

describe("TimeConstraintsCell", () => {
  it("renders 'Forever' for undefined value", () => {
    render(<TimeConstraintsCell value={undefined} />);
    expect(screen.getByText("Forever")).toBeInTheDocument();
    expect(screen.queryByText("—")).not.toBeInTheDocument();
  });

  it("renders 'Forever' when all three families are empty", () => {
    render(<TimeConstraintsCell value={{}} />);
    expect(screen.getByText("Forever")).toBeInTheDocument();
    expect(screen.queryByText("—")).not.toBeInTheDocument();
  });

  it("renders all three blocks with full data using inline label/value layout", () => {
    const { container } = render(
      <TimeConstraintsCell
        value={{
          weekdays: [{ weekdays: [1, 2, 3, 4, 5] }],
          time: [{ from: "09:00", until: "17:00" }],
          datetime: [{ from: "2026-01-01T08:00:00Z", until: "2026-01-02T18:00:00Z" }],
        }}
      />,
    );
    expect(screen.getByText("Weekdays")).toBeInTheDocument();
    expect(screen.getByText("Hours")).toBeInTheDocument();
    expect(screen.getByText("Dates")).toBeInTheDocument();
    expect(screen.getByText("Mon · Tue · Wed · Thu · Fri")).toBeInTheDocument();
    expect(screen.getByText("09:00 – 17:00")).toBeInTheDocument();
    expect(
      screen.getByText("2026-01-01 08:00 → 2026-01-02 18:00"),
    ).toBeInTheDocument();

    // Inline layout: each label sits in the same block as its value(s),
    // and label appears before value in DOM order (label-left, value-right).
    const weekdaysLabel = screen.getByText("Weekdays");
    const weekdaysValue = screen.getByText("Mon · Tue · Wed · Thu · Fri");
    expect(
      weekdaysLabel.compareDocumentPosition(weekdaysValue) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    // Label and its value column are siblings under the same block.
    const hoursLabel = screen.getByText("Hours");
    const hoursValue = screen.getByText("09:00 – 17:00");
    expect(hoursLabel.parentElement).toBe(hoursValue.parentElement?.parentElement);
    expect(container).toBeTruthy();
  });

  it("stacks multiple value lines vertically inside a single Hours block", () => {
    render(
      <TimeConstraintsCell
        value={{
          time: [
            { from: "09:00", until: "12:00" },
            { from: "13:00", until: "17:00" },
          ],
        }}
      />,
    );
    // Both ranges share the same value-column parent (sibling of the label).
    const a = screen.getByText("09:00 – 12:00");
    const b = screen.getByText("13:00 – 17:00");
    expect(a.parentElement).toBe(b.parentElement);
    // The label "Hours" is a sibling of that shared value column, not its ancestor.
    const label = screen.getByText("Hours");
    expect(label.parentElement).toBe(a.parentElement?.parentElement);
  });

  it("renders 'Every day' when all seven weekdays are present", () => {
    render(
      <TimeConstraintsCell
        value={{ weekdays: [{ weekdays: [0, 1, 2, 3, 4, 5, 6] }] }}
      />,
    );
    expect(screen.getByText("Every day")).toBeInTheDocument();
  });

  it("sorts weekdays into Sunday-first order", () => {
    render(
      <TimeConstraintsCell
        value={{ weekdays: [{ weekdays: [3, 0, 6, 1] }] }}
      />,
    );
    expect(screen.getByText("Sun · Mon · Wed · Sat")).toBeInTheDocument();
  });

  it("omits the weekdays block when weekdays are empty", () => {
    render(
      <TimeConstraintsCell
        value={{ time: [{ from: "09:00", until: "17:00" }] }}
      />,
    );
    expect(screen.queryByText("Weekdays")).not.toBeInTheDocument();
    expect(screen.getByText("Hours")).toBeInTheDocument();
  });

  it("omits the hours block when time is empty", () => {
    render(
      <TimeConstraintsCell
        value={{ weekdays: [{ weekdays: [1] }] }}
      />,
    );
    expect(screen.queryByText("Hours")).not.toBeInTheDocument();
    expect(screen.getByText("Weekdays")).toBeInTheDocument();
  });

  it("omits the dates block when datetime is empty", () => {
    render(
      <TimeConstraintsCell
        value={{ weekdays: [{ weekdays: [1] }] }}
      />,
    );
    expect(screen.queryByText("Dates")).not.toBeInTheDocument();
  });

  it("renders each time range on its own line", () => {
    render(
      <TimeConstraintsCell
        value={{
          time: [
            { from: "09:00", until: "12:00" },
            { from: "13:00", until: "17:00" },
          ],
        }}
      />,
    );
    expect(screen.getByText("09:00 – 12:00")).toBeInTheDocument();
    expect(screen.getByText("13:00 – 17:00")).toBeInTheDocument();
  });

  it("renders half-open hours with from/until prefix", () => {
    render(
      <TimeConstraintsCell
        value={{
          time: [{ from: "09:00" }, { until: "17:00" }],
        }}
      />,
    );
    expect(screen.getByText("from 09:00")).toBeInTheDocument();
    expect(screen.getByText("until 17:00")).toBeInTheDocument();
  });

  it("strips seconds from time ranges with HH:MM:SS input", () => {
    render(
      <TimeConstraintsCell
        value={{ time: [{ from: "09:00:00", until: "17:30:00" }] }}
      />,
    );
    expect(screen.getByText("09:00 – 17:30")).toBeInTheDocument();
  });

  it("renders each date range on its own line", () => {
    render(
      <TimeConstraintsCell
        value={{
          datetime: [
            { from: "2026-01-01T08:00:00Z", until: "2026-01-02T18:00:00Z" },
            { from: "2026-02-10T00:00:00Z", until: "2026-02-11T23:59:00Z" },
          ],
        }}
      />,
    );
    expect(
      screen.getByText("2026-01-01 08:00 → 2026-01-02 18:00"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("2026-02-10 00:00 → 2026-02-11 23:59"),
    ).toBeInTheDocument();
  });

  it("renders half-open date ranges with from/until prefix", () => {
    render(
      <TimeConstraintsCell
        value={{
          datetime: [
            { from: "2026-01-01T08:00:00Z" },
            { until: "2026-02-01T18:00:00Z" },
          ],
        }}
      />,
    );
    expect(screen.getByText("from 2026-01-01 08:00")).toBeInTheDocument();
    expect(screen.getByText("until 2026-02-01 18:00")).toBeInTheDocument();
  });
});

describe("summarizeTimeConstraints (unchanged contract)", () => {
  it("still returns its short form for a representative input", () => {
    expect(
      summarizeTimeConstraints({
        weekdays: [{ weekdays: [1, 2, 3, 4, 5] }],
        time: [{ from: "09:00", until: "18:00" }],
      }),
    ).toBe("Mon,Tue,Wed,Thu,Fri · 09:00-18:00");
  });
});
