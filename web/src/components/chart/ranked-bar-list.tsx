import { useMemo } from "react";
import { Empty, SkeletonLine } from "@cloudflare/kumo";

export type RankedBarRow = {
  key: string;
  label: string;
  value: number;
  /** Optional context rendered before the value, e.g. "37 hosts". */
  secondary?: string;
};

export type RankedBarListProps = {
  rows: RankedBarRow[];
  /** Denominator for the share column; pass the unclipped total, not the sum of `rows`. */
  total: number;
  valueFormat?: (value: number) => string;
  /** When provided, each row becomes a button; the folded "Other" row stays inert. */
  onSelect?: (key: string) => void;
  /** Rows kept before the remainder folds into "Other". */
  maxRows?: number;
  emptyLabel?: string;
  loading?: boolean;
};

/** Reserved key for the folded remainder row; never collides with a service name. */
const OTHER_KEY = "__other__";

const SKELETON_ROWS = 5;

const defaultValueFormat = (value: number) => value.toLocaleString();

function formatShare(value: number, total: number): string {
  if (total <= 0) return "";
  const share = (value / total) * 100;
  if (share > 0 && share < 0.1) return "<0.1%";
  return `${share.toFixed(share >= 10 ? 0 : 1)}%`;
}

/**
 * Ranked breakdown drawn in the DOM rather than on a canvas.
 *
 * Magnitude is encoded by bar length, so a single hue is correct and there is no
 * colour-vision exposure at all; every row is directly labelled with its name,
 * value and share, so nothing is colour-alone. DOM also buys real focus order
 * and a real click target for drill-down, which a canvas would have to fake.
 *
 * Bar length is relative to the largest visible row so the ranking stays legible
 * when one row dominates; the share column is relative to `total`.
 */
export function RankedBarList({
  rows,
  total,
  valueFormat = defaultValueFormat,
  onSelect,
  maxRows = 10,
  emptyLabel = "No activity in this range",
  loading = false
}: RankedBarListProps) {
  const ranked = useMemo(() => {
    const sorted = [...rows].sort((a, b) => b.value - a.value);
    if (sorted.length <= maxRows) return sorted;
    const rest = sorted.slice(maxRows);
    const restValue = rest.reduce((sum, row) => sum + row.value, 0);
    return [
      ...sorted.slice(0, maxRows),
      { key: OTHER_KEY, label: "Other", value: restValue, secondary: `${rest.length} more` }
    ];
  }, [rows, maxRows]);

  if (loading) {
    return (
      <div className="flex flex-col gap-1.5 px-2 py-1.5" aria-busy="true">
        {Array.from({ length: SKELETON_ROWS }, (_, index) => (
          <SkeletonLine key={index} blockHeight={20} />
        ))}
      </div>
    );
  }

  if (ranked.length === 0) {
    return (
      <div className="flex min-h-36 items-center justify-center p-4">
        <Empty size="sm" title={emptyLabel} />
      </div>
    );
  }

  const peak = ranked.reduce((max, row) => Math.max(max, row.value), 0);

  return (
    <ul className="flex flex-col">
      {ranked.map((row) => {
        const share = formatShare(row.value, total);
        const width = peak > 0 && row.value > 0 ? Math.max((row.value / peak) * 100, 1) : 0;
        const selectable = Boolean(onSelect) && row.key !== OTHER_KEY;
        const body = (
          <>
            <span
              aria-hidden="true"
              className="absolute inset-y-0.5 left-0 rounded-sm bg-kumo-info/15"
              style={{ width: `${width}%` }}
            />
            <span className="relative min-w-0 truncate text-kumo-default" title={row.label}>
              {row.label}
            </span>
            <span className="relative ml-auto flex shrink-0 items-baseline gap-2 tabular-nums">
              {row.secondary ? <span className="text-xs text-kumo-subtle">{row.secondary}</span> : null}
              <span className="text-kumo-default">{valueFormat(row.value)}</span>
              {share ? <span className="w-11 text-right text-xs text-kumo-subtle">{share}</span> : null}
            </span>
          </>
        );
        const layout = "relative flex w-full items-center gap-3 overflow-hidden rounded-sm px-2 py-1.5 text-left text-sm";
        return (
          <li key={row.key}>
            {selectable ? (
              <button
                type="button"
                onClick={() => onSelect?.(row.key)}
                className={`${layout} transition-colors hover:bg-kumo-tint focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-kumo-brand`}
              >
                {body}
              </button>
            ) : (
              <div className={layout}>{body}</div>
            )}
          </li>
        );
      })}
    </ul>
  );
}
