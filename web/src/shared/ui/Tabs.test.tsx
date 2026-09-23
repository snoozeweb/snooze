import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";
import { TabList, TabPanel, TabTrigger, Tabs } from "./Tabs";

describe("Tabs", () => {
  it("reports one click once, even while the controlled value lags behind", async () => {
    // Radix fires onValueChange on mouse-down and again on the focus that
    // follows. A URL-backed strip whose value has not caught up yet turned
    // that second call into a second, identical history entry.
    const user = userEvent.setup();
    const seen: string[] = [];
    render(
      <Tabs value="rules" onValueChange={(v) => seen.push(v)}>
        <TabList>
          <TabTrigger value="rules">Rules</TabTrigger>
          <TabTrigger value="aggregates">Aggregates</TabTrigger>
        </TabList>
      </Tabs>,
    );
    await user.click(screen.getByRole("tab", { name: "Aggregates" }));
    expect(seen).toEqual(["aggregates"]);

    // The value never moved (a host refused the switch): clicking again later
    // is a new request and gets through.
    await user.click(screen.getByRole("tab", { name: "Aggregates" }));
    expect(seen).toEqual(["aggregates", "aggregates"]);
  });

  it("renders the default tab's panel and switches on click", async () => {
    const user = userEvent.setup();
    render(
      <Tabs defaultValue="rules">
        <TabList>
          <TabTrigger value="rules">Rules</TabTrigger>
          <TabTrigger value="aggregates">Aggregates</TabTrigger>
        </TabList>
        <TabPanel value="rules">Rules content</TabPanel>
        <TabPanel value="aggregates">Aggregates content</TabPanel>
      </Tabs>,
    );
    expect(screen.getByText("Rules content")).toBeInTheDocument();
    expect(screen.queryByText("Aggregates content")).toBeNull();
    await user.click(screen.getByRole("tab", { name: "Aggregates" }));
    expect(screen.getByText("Aggregates content")).toBeInTheDocument();
    expect(screen.queryByText("Rules content")).toBeNull();
  });

  describe("overflow edges", () => {
    // jsdom lays nothing out, so the strip's geometry is stubbed on the
    // prototype for these cases and restored after each one.
    const props = ["scrollWidth", "clientWidth", "scrollLeft"] as const;
    const saved = props.map((p) => Object.getOwnPropertyDescriptor(HTMLElement.prototype, p));
    afterEach(() => {
      props.forEach((p, i) => {
        const d = saved[i];
        if (d) Object.defineProperty(HTMLElement.prototype, p, d);
        else delete (HTMLElement.prototype as unknown as Record<string, unknown>)[p];
      });
    });
    function stubGeometry(scrollWidth: number, clientWidth: number) {
      let left = 0;
      Object.defineProperty(HTMLElement.prototype, "scrollWidth", {
        configurable: true,
        get: () => scrollWidth,
      });
      Object.defineProperty(HTMLElement.prototype, "clientWidth", {
        configurable: true,
        get: () => clientWidth,
      });
      Object.defineProperty(HTMLElement.prototype, "scrollLeft", {
        configurable: true,
        get: () => left,
        set: (v: number) => {
          left = v;
        },
      });
    }
    function renderStrip() {
      render(
        <Tabs defaultValue="a">
          <TabList>
            <TabTrigger value="a">Timeline</TabTrigger>
            <TabTrigger value="b">Record</TabTrigger>
          </TabList>
        </Tabs>,
      );
      return screen.getByRole("tablist");
    }

    it("fades the edge a clipped strip continues past, so the hidden tabs are findable", () => {
      stubGeometry(500, 200);
      const list = renderStrip();
      expect(list).toHaveAttribute("data-fade-end");
      expect(list).not.toHaveAttribute("data-fade-start");

      list.scrollLeft = 300;
      fireEvent.scroll(list);
      expect(list).toHaveAttribute("data-fade-start");
      expect(list).not.toHaveAttribute("data-fade-end");
    });

    it("leaves a strip that fits alone", () => {
      stubGeometry(200, 200);
      const list = renderStrip();
      expect(list).not.toHaveAttribute("data-fade-end");
      expect(list).not.toHaveAttribute("data-fade-start");
    });
  });
});
