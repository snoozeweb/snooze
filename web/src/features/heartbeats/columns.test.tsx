import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { DataTable } from "@/shared/ui/DataTable";
import type { Heartbeat } from "./types";
import { heartbeatColumns } from "./columns";

const fixture: Heartbeat = {
  uid: "hb1",
  name: "hb1",
  interval: 60,
  grace: 30,
  status: "overdue",
  last_seen: "2026-06-30T10:00:00Z",
};

describe("heartbeatColumns", () => {
  it("renders name, status badge, interval + grace, and relative last_seen", () => {
    render(
      <DataTable<Heartbeat>
        data={[fixture]}
        columns={heartbeatColumns}
        rowKey={(r) => r.uid ?? r.name}
      />,
    );

    // Name rendered as code
    expect(screen.getByText("hb1")).toBeInTheDocument();

    // Status badge
    expect(screen.getByText("overdue")).toBeInTheDocument();

    // Interval + grace — secondsToHuman(60) = "1m", secondsToHuman(30) = "30s"
    expect(screen.getByText(/1m.*grace/i)).toBeInTheDocument();

    // last_seen renders a relative time (not the literal ISO string)
    expect(screen.queryByText("2026-06-30T10:00:00Z")).toBeNull();
  });

  it("renders 'never' when last_seen is absent", () => {
    const noSeen: Heartbeat = { uid: "hb2", name: "hb2", interval: 60 };
    render(
      <DataTable<Heartbeat>
        data={[noSeen]}
        columns={heartbeatColumns}
        rowKey={(r) => r.uid ?? r.name}
      />,
    );
    expect(screen.getByText("never")).toBeInTheDocument();
  });

  it("renders latency as '350 ms / 2000 ms' when both values present", () => {
    const withLatency: Heartbeat = {
      uid: "hb3",
      name: "hb3",
      interval: 60,
      last_latency: 350,
      max_latency: 2000,
    };
    render(
      <DataTable<Heartbeat>
        data={[withLatency]}
        columns={heartbeatColumns}
        rowKey={(r) => r.uid ?? r.name}
      />,
    );
    expect(screen.getByText("350 ms / 2000 ms")).toBeInTheDocument();
  });

  it("renders '—' in latency column when last_latency is absent", () => {
    const noLatency: Heartbeat = { uid: "hb4", name: "hb4", interval: 60 };
    render(
      <DataTable<Heartbeat>
        data={[noLatency]}
        columns={heartbeatColumns}
        rowKey={(r) => r.uid ?? r.name}
      />,
    );
    expect(screen.getByText("—")).toBeInTheDocument();
  });
});
