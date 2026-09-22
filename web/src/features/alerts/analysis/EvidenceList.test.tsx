import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { EvidenceList } from "./EvidenceList";

describe("EvidenceList", () => {
  it("renders nothing when there is no evidence", () => {
    const { container } = render(<EvidenceList items={[]} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("keeps the analysis's own order", () => {
    render(<EvidenceList items={["first", "second", "third"]} />);
    const items = screen.getAllByRole("listitem").map((li) => li.textContent);
    expect(items).toEqual(["first", "second", "third"]);
  });

  it("sets a space-free prefix before the first ': ' in mono — it is a command", () => {
    const { container } = render(<EvidenceList items={["journalctl: 4.2G of logs under /var"]} />);
    expect(container.querySelector("code")).toHaveTextContent("journalctl");
    // The rest stays prose, and the whole line still reads as it was written.
    expect(screen.getByRole("listitem")).toHaveTextContent("journalctl: 4.2G of logs under /var");
  });

  it("leaves prose alone — a prefix with a space is a sentence, not a command", () => {
    const { container } = render(
      <EvidenceList items={["Root cause: the journal grew unbounded"]} />,
    );
    expect(container.querySelector("code")).toBeNull();
    expect(screen.getByRole("listitem")).toHaveTextContent(
      "Root cause: the journal grew unbounded",
    );
  });

  it("sets a flagged command in mono — the '-' is what makes it a command", () => {
    const { container } = render(<EvidenceList items={["df -h: /var at 100%"]} />);
    expect(container.querySelector("code")).toHaveTextContent("df -h");
  });

  it("sets a command with a path argument in mono", () => {
    const { container } = render(<EvidenceList items={["df -h /var: 100%"]} />);
    expect(container.querySelector("code")).toHaveTextContent("df -h /var");
  });

  it("sets a short lowercase invocation in mono", () => {
    const { container } = render(<EvidenceList items={["kubectl get pods: 3/5 Running"]} />);
    expect(container.querySelector("code")).toHaveTextContent("kubectl get pods");
  });

  it("sets a metric name in mono — the underscores make the shape", () => {
    const { container } = render(
      <EvidenceList items={["container_memory_max_usage_bytes: 4.2e9"]} />,
    );
    expect(container.querySelector("code")).toHaveTextContent("container_memory_max_usage_bytes");
  });

  it("leaves a capitalised one-word lead-in as prose — 'Note' is not a command", () => {
    // The old rule promoted ANY space-free prefix, so an ordinary annotation
    // was set in mono and the reader stopped trusting the distinction.
    const { container } = render(<EvidenceList items={["Note: the unit flapped twice"]} />);
    expect(container.querySelector("code")).toBeNull();
  });

  it("leaves 'See: https://…' as prose even though the rest holds slashes", () => {
    const { container } = render(<EvidenceList items={["See: https://grafana.egerie.eu/d/abc"]} />);
    expect(container.querySelector("code")).toBeNull();
  });

  it("leaves a hyphenated capitalised lead-in as prose", () => {
    const { container } = render(<EvidenceList items={["Well-known issue: the cron overlaps"]} />);
    expect(container.querySelector("code")).toBeNull();
  });

  it("leaves a lowercase sentence lead-in as prose — too many words to be a command", () => {
    const { container } = render(
      <EvidenceList items={["the unit restarted twice: within the window"]} />,
    );
    expect(container.querySelector("code")).toBeNull();
  });

  it("leaves a line with no ': ' alone", () => {
    const { container } = render(<EvidenceList items={["the unit restarted twice"]} />);
    expect(container.querySelector("code")).toBeNull();
  });

  it("leaves a line that merely opens with ': ' alone — an empty prefix names nothing", () => {
    const { container } = render(<EvidenceList items={[": leading separator"]} />);
    expect(container.querySelector("code")).toBeNull();
  });

  it("splits on the FIRST separator, so a colon in the output does not re-split", () => {
    const { container } = render(<EvidenceList items={["kubectl: pod A: CrashLoopBackOff"]} />);
    expect(container.querySelectorAll("code")).toHaveLength(1);
    expect(container.querySelector("code")).toHaveTextContent("kubectl");
    expect(screen.getByRole("listitem")).toHaveTextContent("kubectl: pod A: CrashLoopBackOff");
  });
});
