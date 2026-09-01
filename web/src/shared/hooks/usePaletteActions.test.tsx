import { render, screen } from "@testing-library/react";
import { useMemo } from "react";
import { describe, expect, it } from "vitest";
import {
  usePaletteActions,
  usePublishPaletteActions,
  type PaletteAction,
} from "./usePaletteActions";

function Publisher({ labels }: { labels: string[] }) {
  const list = useMemo<PaletteAction[]>(
    () => labels.map((label) => ({ id: label, label, run: () => undefined })),
    [labels],
  );
  usePublishPaletteActions(list);
  return null;
}

function Reader() {
  const actions = usePaletteActions();
  return <div data-testid="out">{actions.map((a) => a.label).join(",")}</div>;
}

describe("palette action registry", () => {
  it("publishes while mounted and withdraws on unmount", () => {
    const { rerender } = render(
      <>
        <Reader />
        <Publisher labels={["Acknowledge selected"]} />
      </>,
    );
    expect(screen.getByTestId("out")).toHaveTextContent("Acknowledge selected");

    rerender(
      <>
        <Reader />
      </>,
    );
    expect(screen.getByTestId("out")).toHaveTextContent("");
  });

  it("re-publishes when the memoized list changes", () => {
    function Wrap({ labels }: { labels: string[] }) {
      return (
        <>
          <Reader />
          <Publisher labels={labels} />
        </>
      );
    }
    const { rerender } = render(<Wrap labels={["a"]} />);
    expect(screen.getByTestId("out")).toHaveTextContent("a");
    rerender(<Wrap labels={["b", "c"]} />);
    expect(screen.getByTestId("out")).toHaveTextContent("b,c");
  });
});
