// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { RankedBarList } from "./ranked-bar-list";

// Vitest runs without globals, so Testing Library never registers auto-cleanup.
afterEach(cleanup);

const rows = [
  { key: "youtube", label: "YouTube", value: 400, secondary: "37 hosts" },
  { key: "github", label: "GitHub", value: 300 },
  { key: "telegram", label: "Telegram", value: 200 },
  { key: "netflix", label: "Netflix", value: 100 }
];

describe("RankedBarList", () => {
  it("orders rows by value and shows each share of the supplied total", () => {
    render(<RankedBarList rows={[...rows].reverse()} total={2000} />);
    const items = screen.getAllByRole("listitem").map((item) => item.textContent);
    expect(items).toEqual(["YouTube37 hosts40020%", "GitHub30015%", "Telegram20010%", "Netflix1005.0%"]);
  });

  it("folds the remainder into an inert Other row", () => {
    render(<RankedBarList rows={rows} total={1000} maxRows={2} onSelect={() => {}} />);
    const buttons = screen.getAllByRole("button").map((button) => button.textContent);
    expect(buttons).toEqual(["YouTube37 hosts40040%", "GitHub30030%"]);
    const last = screen.getAllByRole("listitem").at(-1)!;
    expect(last.textContent).toBe("Other2 more30030%");
    expect(last.querySelector("button")).toBeNull();
  });

  it("renders keyboard-reachable buttons that report their key", () => {
    const onSelect = vi.fn();
    render(<RankedBarList rows={rows} total={1000} onSelect={onSelect} />);
    const button = screen.getByRole("button", { name: /GitHub/ });
    expect(button.getAttribute("type")).toBe("button");
    button.click();
    expect(onSelect).toHaveBeenCalledWith("github");
  });

  it("renders plain rows with no interactive affordance when onSelect is omitted", () => {
    render(<RankedBarList rows={rows} total={1000} />);
    expect(screen.queryAllByRole("button")).toHaveLength(0);
  });

  it("scales bar width against the largest visible row, not the total", () => {
    const { container } = render(<RankedBarList rows={rows} total={2000} />);
    const widths = [...container.querySelectorAll<HTMLElement>("[aria-hidden='true']")].map(
      (bar) => bar.style.width
    );
    expect(widths).toEqual(["100%", "75%", "50%", "25%"]);
  });

  it("suppresses the share column when the total is unknown", () => {
    render(<RankedBarList rows={[rows[0]]} total={0} />);
    expect(screen.getByRole("listitem").textContent).toBe("YouTube37 hosts400");
  });

  it("shows the empty state instead of an empty list", () => {
    render(<RankedBarList rows={[]} total={0} emptyLabel="No services seen" />);
    expect(screen.queryByRole("list")).toBeNull();
    expect(screen.getByText("No services seen")).toBeTruthy();
  });

  it("shows a busy skeleton while loading", () => {
    const { container } = render(<RankedBarList rows={rows} total={1000} loading />);
    expect(container.querySelector("[aria-busy='true']")).toBeTruthy();
    expect(screen.queryByRole("list")).toBeNull();
  });
});
