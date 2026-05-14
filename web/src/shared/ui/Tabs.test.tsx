import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { TabList, TabPanel, TabTrigger, Tabs } from "./Tabs";

describe("Tabs", () => {
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
});
