import { useMemo } from "react";
import { TimeseriesChart } from "@cloudflare/kumo";
import { format } from "date-fns";

import { seriesColors } from "./chart-palette";
import { echarts } from "./echarts";
import { useIsDarkMode } from "./use-color-mode";

export type TimeBucket = "hour" | "day";

export type TimeSeries = {
  key: string;
  label: string;
  /** `[bucket_start_ms, value]` tuples, ordered and zero-filled by the server. */
  points: [number, number][];
};

export type TimeBarChartProps = {
  /**
   * One or two series. Only two colours are validated for adjacency, so a
   * caller with more groups folds the remainder into an "Other" series before
   * passing them here.
   */
  series: TimeSeries[];
  /** Bucket width, which decides the x-axis tick format. */
  bucket: TimeBucket;
  /** Chart height in pixels. */
  height?: number;
  loading?: boolean;
  /** Formats y-axis ticks and tooltip values, e.g. `formatBytes`. */
  valueFormat?: (value: number) => string;
  yAxisName?: string;
  /** Required: announced by screen readers when the chart takes focus. */
  ariaDescription: string;
  /** Drag-to-select over the time axis; both bounds are epoch milliseconds. */
  onTimeRangeChange?: (fromMs: number, toMs: number) => void;
};

/**
 * Bucketed time histogram over a server-computed series.
 *
 * Buckets and zero-fill belong to the server — this component plots exactly the
 * points it is given and never derives its own buckets. Kumo renders bar series
 * with `stack: "total"` unconditionally, so two series always stack; that is
 * what every current caller wants (uplink over downlink, one bar per bucket).
 */
export function TimeBarChart({
  series,
  bucket,
  height = 200,
  loading = false,
  valueFormat,
  yAxisName,
  ariaDescription,
  onTimeRangeChange
}: TimeBarChartProps) {
  const isDark = useIsDarkMode();

  const data = useMemo(() => {
    const colors = seriesColors(isDark);
    return series.map((entry, index) => ({
      name: entry.label,
      data: entry.points,
      color: colors[index % colors.length]
    }));
  }, [series, isDark]);

  const tickFormat = useMemo(() => {
    const pattern = bucket === "hour" ? "HH:mm" : "MMM d";
    return (value: number) => format(value, pattern);
  }, [bucket]);

  return (
    <TimeseriesChart
      echarts={echarts}
      type="bar"
      data={data}
      height={height}
      loading={loading}
      isDarkMode={isDark}
      ariaDescription={ariaDescription}
      yAxisName={yAxisName}
      yAxisTickFormat={valueFormat}
      tooltipValueFormat={valueFormat}
      xAxisTickFormat={tickFormat}
      tooltipFollowCursor="x"
      onTimeRangeChange={onTimeRangeChange}
    />
  );
}
