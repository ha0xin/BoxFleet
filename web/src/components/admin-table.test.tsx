// @vitest-environment jsdom

import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { TableColgroup, tableMinWidth, type TableColumnWidth } from "./admin-table";

// Vitest runs without globals, so Testing Library never registers auto-cleanup.
afterEach(cleanup);

function cols(widths: readonly TableColumnWidth[]): HTMLTableColElement[] {
  const { container } = render(
    <table>
      <TableColgroup widths={widths} />
    </table>
  );
  return [...container.querySelectorAll("col")];
}

describe("tableMinWidth", () => {
  it("sums a table with no flexible columns", () => {
    expect(tableMinWidth([120, 96, 52])).toBe(268);
  });

  it("adds one floor per flexible column", () => {
    expect(tableMinWidth([{ min: 100 }, 120, { min: 100 }])).toBe(320);
  });

  // Fixed table layout splits the leftover equally, so the table only honours
  // every floor once it is wide enough for the *largest* floor to be met in each
  // flexible column. Summing the individual floors would under-report that.
  it("reserves the largest flexible floor for every flexible column", () => {
    expect(tableMinWidth([{ min: 100 }, { min: 240 }, 60])).toBe(540);
  });

  it("is zero for an empty column set", () => {
    expect(tableMinWidth([])).toBe(0);
  });
});

describe("TableColgroup", () => {
  it("renders one col per column, in order", () => {
    expect(cols([100, { min: 80 }, 52])).toHaveLength(3);
  });

  it("gives fixed columns an exact px width", () => {
    const [first] = cols([144, { min: 80 }]);
    expect(first.style.width).toBe("144px");
  });

  // Flexible columns must stay `auto`: that is what makes the browser hand them
  // the leftover width instead of pinning them.
  it("leaves flexible columns without a width", () => {
    const [, second] = cols([144, { min: 80 }]);
    expect(second.style.width).toBe("");
  });
});
