// @vitest-environment jsdom

import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { Sparkline } from "./sparkline";

// Vitest runs without globals, so Testing Library never registers auto-cleanup.
afterEach(cleanup);

function paths(container: HTMLElement): SVGPathElement[] {
  return [...container.querySelectorAll("path")];
}

/** Parses the `y` of every vertex out of an `M x,y L x,y ...` line path. */
function lineYs(d: string): number[] {
  return d
    .slice(1)
    .split("L")
    .map((point) => Number(point.split(",")[1]));
}

describe("Sparkline", () => {
  it("renders nothing for empty data", () => {
    const { container } = render(<Sparkline values={[]} label="Traffic" />);
    expect(container.firstChild).toBeNull();
  });

  it("renders nothing for a single point", () => {
    const { container } = render(<Sparkline values={[42]} label="Traffic" />);
    expect(container.firstChild).toBeNull();
  });

  it("renders nothing when a value is not finite", () => {
    const { container } = render(<Sparkline values={[1, Number.NaN, 3]} label="Traffic" />);
    expect(container.firstChild).toBeNull();
  });

  it("draws all-equal values as a flat line at the vertical centre", () => {
    const { container } = render(<Sparkline values={[7, 7, 7]} label="Traffic" height={24} />);
    const line = paths(container).at(-1)!;
    const ys = lineYs(line.getAttribute("d")!);
    expect(ys).toHaveLength(3);
    expect(new Set(ys).size).toBe(1);
    expect(ys[0]).toBe(12);
  });

  it("maps the minimum to the bottom and the maximum to the top of the inset box", () => {
    const { container } = render(
      <Sparkline values={[0, 5, 10]} label="Traffic" width={100} height={24} strokeWidth={2} />
    );
    const line = paths(container).at(-1)!;
    expect(line.getAttribute("d")).toBe("M0,22L50,12L100,2");
  });

  it("renders no id-bearing defs so instances cannot collide in a list", () => {
    const { container } = render(<Sparkline values={[1, 2, 3]} label="Traffic" />);
    expect(container.querySelector("defs")).toBeNull();
    expect(container.querySelector("linearGradient")).toBeNull();
    expect(container.querySelector("[id]")).toBeNull();
  });

  it("exposes the label to assistive technology and scales without measurement", () => {
    const { container } = render(<Sparkline values={[1, 4, 2]} label="Node throughput" />);
    const svg = container.querySelector("svg")!;
    expect(svg.getAttribute("role")).toBe("img");
    expect(svg.getAttribute("aria-label")).toBe("Node throughput");
    expect(svg.getAttribute("preserveAspectRatio")).toBe("none");
    expect(svg.getAttribute("viewBox")).toBe("0 0 100 24");
  });

  it("draws the area fill only when asked and keeps the stroke non-scaling", () => {
    const withArea = render(<Sparkline values={[1, 2, 3]} label="Traffic" />);
    expect(paths(withArea.container)).toHaveLength(2);

    const withoutArea = render(<Sparkline values={[1, 2, 3]} label="Traffic" area={false} />);
    const [line] = paths(withoutArea.container);
    expect(paths(withoutArea.container)).toHaveLength(1);
    expect(line.getAttribute("vector-effect")).toBe("non-scaling-stroke");
    expect(line.getAttribute("stroke")).toBe("currentColor");
  });

  it("closes the area path down to the baseline", () => {
    const { container } = render(
      <Sparkline values={[1, 3]} label="Traffic" width={40} height={20} strokeWidth={2} />
    );
    const [area] = paths(container);
    expect(area.getAttribute("d")).toBe("M0,18L40,2L40,20L0,20Z");
    expect(area.getAttribute("fill")).toBe("currentColor");
  });

  it("carries the caller's colour class onto the svg", () => {
    const { container } = render(
      <Sparkline values={[1, 2]} label="Traffic" className="text-kumo-success" />
    );
    expect(container.querySelector("svg")!.getAttribute("class")).toBe(
      "block h-full w-full text-kumo-success"
    );
  });
});
