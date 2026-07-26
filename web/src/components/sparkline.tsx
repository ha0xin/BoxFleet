export type SparklineProps = {
  /** Values oldest-first. Fewer than two finite values renders nothing. */
  values: readonly number[];
  /** Screen-reader name for the trend, e.g. "Billable traffic, last 7 days". */
  label: string;
  /** viewBox width in user units; the rendered width comes from CSS. */
  width?: number;
  /** viewBox height in user units; the rendered height comes from CSS. */
  height?: number;
  /** Stroke width in device pixels — the stroke does not scale with the box. */
  strokeWidth?: number;
  /** Translucent fill under the line. */
  area?: boolean;
  /** Carries the colour, e.g. `text-kumo-info`; the mark uses `currentColor`. */
  className?: string;
};

function round(value: number): number {
  return Math.round(value * 100) / 100;
}

/**
 * Inline-SVG trend line sized entirely by CSS.
 *
 * `preserveAspectRatio="none"` means the box never has to be measured, so a
 * sparkline costs no `ResizeObserver` and no effect even in a paginated table,
 * and `vectorEffect="non-scaling-stroke"` keeps the line a true `strokeWidth`
 * device pixels under the resulting non-uniform scale. There is deliberately no
 * `<defs>` and no gradient: an id would have to be unique per instance, which is
 * a duplicate-DOM-id bug waiting for the first list that renders rows in a loop.
 */
export function Sparkline({
  values,
  label,
  width = 100,
  height = 24,
  strokeWidth = 1.5,
  area = true,
  className = ""
}: SparklineProps) {
  if (values.length < 2 || !values.every((value) => Number.isFinite(value))) return null;

  // Inset the plot vertically so the stroke is not clipped at the extremes.
  const inset = Math.min(strokeWidth, height / 4);
  const top = inset;
  const bottom = height - inset;
  const min = Math.min(...values);
  const max = Math.max(...values);
  const span = max - min;
  const step = width / (values.length - 1);

  const points = values.map((value, index) => {
    const y = span === 0 ? (top + bottom) / 2 : bottom - ((value - min) / span) * (bottom - top);
    return `${round(index * step)},${round(y)}`;
  });
  const line = `M${points.join("L")}`;

  return (
    <svg
      role="img"
      aria-label={label}
      viewBox={`0 0 ${width} ${height}`}
      preserveAspectRatio="none"
      className={`block h-full w-full ${className}`.trimEnd()}
    >
      {area ? (
        <path
          d={`${line}L${round(width)},${height}L0,${height}Z`}
          fill="currentColor"
          fillOpacity={0.14}
          stroke="none"
        />
      ) : null}
      <path
        d={line}
        fill="none"
        stroke="currentColor"
        strokeWidth={strokeWidth}
        strokeLinecap="round"
        strokeLinejoin="round"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  );
}
