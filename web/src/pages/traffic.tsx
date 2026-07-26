import { useEffect, useMemo, useState } from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import { useQuery } from "@tanstack/react-query";
import {
  createColumnHelper,
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  useReactTable
} from "@tanstack/react-table";
import type { SortingState } from "@tanstack/react-table";
import { useForm } from "react-hook-form";
import { useSearchParams } from "react-router-dom";
import type { DateRange } from "react-day-picker";
import { z } from "zod";
import { ArrowClockwiseIcon, CalendarBlankIcon, FunnelIcon } from "@phosphor-icons/react";
import {
  Badge,
  Banner,
  Button,
  ChartLegend,
  Collapsible,
  Combobox,
  DatePicker,
  Empty,
  LayerCard,
  Popover,
  Table,
  Tabs
} from "@cloudflare/kumo";
import { endOfDay, format, isValid, parseISO, startOfDay, subDays, subHours } from "date-fns";

import type {
  AdminNode,
  AdminUser,
  SeriesBucket,
  TrafficSeries,
  TrafficSeriesGroup,
  TrafficSeriesResponse,
  TrafficVolume
} from "../types";
import { useAdminApi } from "@/admin/api";
import { adminKeys, queryString } from "@/admin/query";
import { SortHead, TableCard, TableEmpty, TableError, TableLoading } from "@/components/admin-table";
import type { SortDirection } from "@/components/admin-table";
import { AppPageHeader } from "@/components/app-page-header";
import { seriesColors } from "@/components/chart/chart-palette";
import { TimeBarChart } from "@/components/chart/time-bar-chart";
import type { TimeSeries } from "@/components/chart/time-bar-chart";
import { useIsDarkMode } from "@/components/chart/use-color-mode";
import { Sparkline } from "@/components/sparkline";
import { formatBytes } from "../utils";

type RangePreset = "24h" | "7d" | "30d" | "90d" | "custom";
type Metric = "billable" | "raw";
type UserSortColumn = "label" | "downlink" | "uplink" | "total";

const HOUR_MS = 3_600_000;
const DAY_MS = 24 * HOUR_MS;

// The server rejects hour buckets past 8 days; stop a day earlier so the UI
// never asks for a window it knows will 422, and because 169 bars is already
// the practical ceiling for a full-width card.
const HOURLY_MAX_SPAN_MS = 7 * DAY_MS;
const AUTO_HOUR_SPAN_MS = 48 * HOUR_MS;
const USER_SERIES_LIMIT = 25;

const rangePresets = [
  ["24h", "Last 24 hours", 1],
  ["7d", "Last 7 days", 7],
  ["30d", "Last 30 days", 30],
  ["90d", "Last 90 days", 90]
] as const;

const filterSchema = z.object({
  range: z.enum(["24h", "7d", "30d", "90d", "custom"]),
  // "" means the granularity is derived from the window span.
  bucket: z.enum(["", "hour", "day"]),
  metric: z.enum(["billable", "raw"]),
  node: z.string(),
  user: z.string()
});

type FilterValues = z.infer<typeof filterSchema>;

const defaultFilters: FilterValues = {
  range: "7d",
  bucket: "",
  metric: "billable",
  node: "all",
  user: "all"
};

const columnHelper = createColumnHelper<UserTrafficRow>();

type UserTrafficRow = {
  key: string;
  label: string;
  uplink: number;
  downlink: number;
  total: number;
  trend: number[];
};

function validRange(value: string | null): RangePreset {
  return value === "24h" || value === "7d" || value === "30d" || value === "90d" || value === "custom"
    ? value
    : defaultFilters.range;
}

function validBucketParam(value: string | null): FilterValues["bucket"] {
  return value === "hour" || value === "day" ? value : "";
}

function validMetric(value: string | null): Metric {
  return value === "raw" ? "raw" : "billable";
}

function parseDateParam(value: string | null): Date | null {
  if (!value) return null;
  const date = parseISO(value);
  return isValid(date) ? date : null;
}

export function filtersFromSearchParams(params: URLSearchParams): FilterValues {
  return {
    range: validRange(params.get("range")),
    bucket: validBucketParam(params.get("bucket")),
    metric: validMetric(params.get("metric")),
    node: params.get("node") ?? "all",
    user: params.get("user") ?? "all"
  };
}

/**
 * Resolves the request window. `start` and `end` are required by the series
 * endpoint, so an unusable custom range falls back to the default preset rather
 * than issuing a request the server will reject.
 */
export function resolveTimeRange(filters: FilterValues, startParam: string | null, endParam: string | null, now: Date) {
  if (filters.range === "custom") {
    const start = parseDateParam(startParam);
    const end = parseDateParam(endParam);
    if (start && end && end.getTime() > start.getTime()) {
      return {
        start: start.toISOString(),
        end: end.toISOString(),
        label: `${format(start, "MMM d")} - ${format(end, "MMM d")}`
      };
    }
  }
  const preset = rangePresets.find(([value]) => value === filters.range) ?? rangePresets[1];
  const [, label, days] = preset;
  const start = days === 1 ? subHours(now, 24) : subDays(now, days);
  return { start: start.toISOString(), end: now.toISOString(), label };
}

function dateRangeFromParams(filters: FilterValues, startParam: string | null, endParam: string | null, now: Date): DateRange {
  const resolved = resolveTimeRange(filters, startParam, endParam, now);
  return { from: parseDateParam(resolved.start) ?? subDays(now, 7), to: parseDateParam(resolved.end) ?? now };
}

/**
 * Hour buckets are only offered while the window is short enough to read; an
 * explicit `hour` request over a longer window degrades to days instead of
 * erroring, and snaps back when the window shrinks again.
 */
export function resolveBucket(requested: FilterValues["bucket"], spanMs: number): SeriesBucket {
  if (spanMs <= HOURLY_MAX_SPAN_MS && requested === "hour") return "hour";
  if (requested === "day" || spanMs > HOURLY_MAX_SPAN_MS) return "day";
  return spanMs <= AUTO_HOUR_SPAN_MS ? "hour" : "day";
}

/**
 * Day buckets are cut on the browser's local midnight, so the server needs the
 * offset it must add to UTC to reach local time — the opposite sign of the
 * JavaScript convention. Hour buckets stay UTC-aligned and send nothing.
 */
export function dayBucketOffsetMinutes(bucket: SeriesBucket, endISO: string): number {
  if (bucket !== "day") return 0;
  const end = new Date(endISO);
  return -(Number.isFinite(end.getTime()) ? end : new Date()).getTimezoneOffset();
}

/** Query parameters shared by every traffic series request on this page. */
export function trafficSeriesScope(options: {
  start: string;
  end: string;
  bucket: SeriesBucket;
  offsetMinutes: number;
  node: string;
  user: string;
  group: TrafficSeriesGroup;
  limit?: number;
}) {
  return {
    start: options.start,
    end: options.end,
    bucket: options.bucket,
    offset_minutes: options.bucket === "day" ? options.offsetMinutes : undefined,
    node: options.node === "all" ? undefined : options.node,
    user: options.user === "all" ? undefined : options.user,
    group: options.group,
    limit: options.limit
  };
}

export function directedBytes(volume: TrafficVolume, metric: Metric) {
  const uplink = metric === "raw" ? volume.uplink_raw_bytes : volume.uplink_billable_bytes;
  const downlink = metric === "raw" ? volume.downlink_raw_bytes : volume.downlink_billable_bytes;
  return { uplink, downlink, total: uplink + downlink };
}

/**
 * Downlink carries the primary colour and is listed first: it dominates volume,
 * so it reads as the base of the stack.
 */
export function trafficChartSeries(series: TrafficSeries | null, metric: Metric): TimeSeries[] {
  if (!series) return [];
  const project = (direction: "downlink" | "uplink"): [number, number][] =>
    series.points.map((point) => [Date.parse(point.bucket_start), directedBytes(point, metric)[direction]]);
  return [
    { key: "downlink", label: "Downlink", points: project("downlink") },
    { key: "uplink", label: "Uplink", points: project("uplink") }
  ];
}

export function trafficUserRows(series: TrafficSeries[], metric: Metric): UserTrafficRow[] {
  return series.map((entry) => {
    const totals = directedBytes(entry.totals, metric);
    return {
      key: entry.key,
      label: entry.label,
      uplink: totals.uplink,
      downlink: totals.downlink,
      total: totals.total,
      trend: entry.points.map((point) => directedBytes(point, metric).total)
    };
  });
}

function metricLabel(metric: Metric): string {
  return metric === "raw" ? "metered" : "billable";
}

export function formatShare(value: number, total: number): string {
  if (total <= 0) return "0%";
  const share = (value / total) * 100;
  if (share > 0 && share < 0.1) return "<0.1%";
  return `${share.toFixed(share >= 10 ? 0 : 1)}%`;
}

function formatBucketStart(value: string, bucket: SeriesBucket): string {
  const date = parseDateParam(value);
  if (!date) return "n/a";
  return format(date, bucket === "hour" ? "MMM d, HH:mm" : "MMM d");
}

export function peakBucket(series: TrafficSeries | null, metric: Metric) {
  if (!series) return null;
  let peak: { bucketStart: string; value: number } | null = null;
  for (const point of series.points) {
    const value = directedBytes(point, metric).total;
    if (!peak || value > peak.value) peak = { bucketStart: point.bucket_start, value };
  }
  return peak;
}

function StatTile({
  label,
  value,
  detail,
  trend
}: {
  label: string;
  value: string;
  detail?: string;
  trend?: number[];
}) {
  return (
    <LayerCard className="flex h-full w-full flex-col">
      <LayerCard.Primary className="flex flex-1 flex-col gap-2 p-4">
        <span className="text-xs font-medium text-kumo-subtle">{label}</span>
        <div className="flex flex-wrap items-baseline gap-x-2">
          <span className="text-xl font-semibold leading-none text-kumo-default tabular-nums">{value}</span>
          {detail ? <span className="text-sm font-medium text-kumo-subtle tabular-nums">{detail}</span> : null}
        </div>
        {trend ? (
          <div className="mt-auto h-8 w-full min-w-0 pt-1 text-kumo-info">
            <Sparkline values={trend} label={`${label} trend`} />
          </div>
        ) : null}
      </LayerCard.Primary>
    </LayerCard>
  );
}

export function TrafficPage() {
  const { request } = useAdminApi();
  const [searchParams, setSearchParams] = useSearchParams();
  const [nowAnchor, setNowAnchor] = useState(() => new Date());
  const [refreshGeneration, setRefreshGeneration] = useState(0);
  const [filterOpen, setFilterOpen] = useState(false);
  const [sorting, setSorting] = useState<SortingState>([{ id: "total", desc: true }]);
  const isDark = useIsDarkMode();

  const filters = useMemo(() => filtersFromSearchParams(searchParams), [searchParams]);
  const startParam = searchParams.get("start");
  const endParam = searchParams.get("end");
  const timeRange = useMemo(
    () => resolveTimeRange(filters, startParam, endParam, nowAnchor),
    [endParam, filters, nowAnchor, startParam]
  );
  const spanMs = Date.parse(timeRange.end) - Date.parse(timeRange.start);
  const bucket = resolveBucket(filters.bucket, spanMs);
  const hourlyAvailable = spanMs <= HOURLY_MAX_SPAN_MS;
  const offsetMinutes = dayBucketOffsetMinutes(bucket, timeRange.end);

  const [draftRange, setDraftRange] = useState<DateRange>(() =>
    dateRangeFromParams(filters, startParam, endParam, nowAnchor)
  );

  const form = useForm<FilterValues>({
    resolver: zodResolver(filterSchema),
    values: filters
  });
  const formValues = form.watch();

  useEffect(() => {
    setDraftRange(dateRangeFromParams(filters, startParam, endParam, nowAnchor));
  }, [endParam, filters, nowAnchor, startParam]);

  function writeParams(values: FilterValues, nextStart = startParam, nextEnd = endParam) {
    if (values.range !== "custom") setNowAnchor(new Date());
    const next = new URLSearchParams();
    if (values.range !== defaultFilters.range) next.set("range", values.range);
    if (values.bucket) next.set("bucket", values.bucket);
    if (values.metric !== defaultFilters.metric) next.set("metric", values.metric);
    if (values.node !== "all") next.set("node", values.node);
    if (values.user !== "all") next.set("user", values.user);
    if (values.range === "custom") {
      if (nextStart) next.set("start", nextStart);
      if (nextEnd) next.set("end", nextEnd);
    }
    setSearchParams(next);
  }

  function setRangePreset(value: RangePreset) {
    form.setValue("range", value);
    if (value !== "custom") writeParams({ ...form.getValues(), range: value }, null, null);
  }

  function applyCustomRange() {
    const from = draftRange.from ? startOfDay(draftRange.from).toISOString() : null;
    const to = draftRange.to
      ? endOfDay(draftRange.to).toISOString()
      : draftRange.from
        ? endOfDay(draftRange.from).toISOString()
        : null;
    writeParams({ ...form.getValues(), range: "custom" }, from, to);
  }

  // Brush-selecting on the chart is the fastest way to zoom into a spike, so it
  // writes the same custom range the date picker does.
  function selectChartRange(fromMs: number, toMs: number) {
    if (!Number.isFinite(fromMs) || !Number.isFinite(toMs) || toMs - fromMs < 60_000) return;
    writeParams(
      { ...form.getValues(), range: "custom" },
      new Date(fromMs).toISOString(),
      new Date(toMs).toISOString()
    );
  }

  function clearFilters() {
    form.reset(defaultFilters);
    setSearchParams(new URLSearchParams());
  }

  const scope = { start: timeRange.start, end: timeRange.end, bucket, offsetMinutes, node: filters.node, user: filters.user };
  // Preset ranges key on the preset rather than their exact millisecond
  // boundaries so revisiting the page reuses the cache; a custom range keeps its
  // exact window in the key.
  const seriesKeyScope = {
    range: filters.range,
    start: filters.range === "custom" ? timeRange.start : undefined,
    end: filters.range === "custom" ? timeRange.end : undefined,
    bucket,
    offsetMinutes,
    node: filters.node,
    user: filters.user,
    refreshGeneration
  };

  const totalQuery = useQuery({
    queryKey: adminKeys.trafficSeries({ ...seriesKeyScope, group: "total" }),
    queryFn: ({ signal }) =>
      request<TrafficSeriesResponse>(
        `/api/admin/traffic/series${queryString(trafficSeriesScope({ ...scope, group: "total" }))}`,
        { signal }
      ),
    placeholderData: (previous) => previous
  });
  const userQuery = useQuery({
    queryKey: adminKeys.trafficSeries({ ...seriesKeyScope, group: "user", limit: USER_SERIES_LIMIT }),
    queryFn: ({ signal }) =>
      request<TrafficSeriesResponse>(
        `/api/admin/traffic/series${queryString(
          trafficSeriesScope({ ...scope, group: "user", limit: USER_SERIES_LIMIT })
        )}`,
        { signal }
      ),
    placeholderData: (previous) => previous
  });
  const nodesQuery = useQuery({
    queryKey: adminKeys.nodes,
    queryFn: ({ signal }) => request<AdminNode[]>("/api/admin/nodes", { signal })
  });
  const usersQuery = useQuery({
    queryKey: adminKeys.users(false),
    queryFn: ({ signal }) => request<AdminUser[]>("/api/admin/users", { signal })
  });

  const totalSeries = totalQuery.data?.series[0] ?? null;
  const windowTotals = totalSeries ? directedBytes(totalSeries.totals, filters.metric) : { uplink: 0, downlink: 0, total: 0 };
  const alternateTotals = totalSeries
    ? directedBytes(totalSeries.totals, filters.metric === "raw" ? "billable" : "raw")
    : { uplink: 0, downlink: 0, total: 0 };
  const peak = peakBucket(totalSeries, filters.metric);
  const totalTrend = useMemo(
    () => (totalSeries ? totalSeries.points.map((point) => directedBytes(point, filters.metric).total) : []),
    [totalSeries, filters.metric]
  );

  const colors = seriesColors(isDark);
  const chartSeries = useMemo(() => trafficChartSeries(totalSeries, filters.metric), [totalSeries, filters.metric]);
  const userRows = useMemo(
    () => trafficUserRows(userQuery.data?.series ?? [], filters.metric),
    [userQuery.data?.series, filters.metric]
  );

  const shareTotal = windowTotals.total > 0 ? windowTotals.total : userRows.reduce((sum, row) => sum + row.total, 0);

  const columns = useMemo(
    () => [
      columnHelper.accessor("label", {
        header: "User",
        cell: (info) => (
          <span className="block max-w-52 truncate font-medium text-kumo-default" title={info.getValue()}>
            {info.getValue()}
          </span>
        )
      }),
      columnHelper.display({
        id: "trend",
        header: "Trend",
        cell: (info) => (
          <div className="h-7 w-32 min-w-0 text-kumo-info">
            <Sparkline
              values={info.row.original.trend}
              label={`${info.row.original.label} traffic trend per ${bucket}`}
            />
          </div>
        )
      }),
      columnHelper.accessor("downlink", {
        header: "Downlink",
        cell: (info) => <span className="whitespace-nowrap text-kumo-default tabular-nums">{formatBytes(info.getValue())}</span>
      }),
      columnHelper.accessor("uplink", {
        header: "Uplink",
        cell: (info) => <span className="whitespace-nowrap text-kumo-default tabular-nums">{formatBytes(info.getValue())}</span>
      }),
      columnHelper.accessor("total", {
        header: "Total",
        cell: (info) => (
          <div className="whitespace-nowrap tabular-nums">
            <div className="text-kumo-default">{formatBytes(info.getValue())}</div>
            <div className="text-xs text-kumo-subtle">{formatShare(info.getValue(), shareTotal)} of window</div>
          </div>
        )
      })
    ],
    [bucket, shareTotal]
  );

  const table = useReactTable({
    data: userRows,
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel()
  });

  const activeSort = (sorting[0]?.id ?? "total") as UserSortColumn;
  const activeDirection: SortDirection = sorting[0]?.desc === false ? "asc" : "desc";
  function setSort(column: UserSortColumn) {
    setSorting([{ id: column, desc: column === activeSort ? activeDirection === "asc" : column !== "label" }]);
  }

  const activeFilterCount = [filters.node !== "all", filters.user !== "all"].filter(Boolean).length;
  const nodeChoices = useMemo(() => ["all", ...(nodesQuery.data ?? []).map((node) => node.name)], [nodesQuery.data]);
  const userChoices = useMemo(() => ["all", ...(usersQuery.data ?? []).map((user) => user.name)], [usersQuery.data]);

  const chartError = totalQuery.error instanceof Error ? totalQuery.error.message : null;
  const userError = userQuery.error instanceof Error ? userQuery.error.message : "Request failed.";
  const chartEmpty = !chartError && !totalQuery.isLoading && windowTotals.total === 0;

  function applyPanelFilters() {
    void form.handleSubmit((values) => {
      writeParams(values);
      setFilterOpen(false);
    })();
  }

  function clearPanelFilters() {
    clearFilters();
    setFilterOpen(false);
  }

  return (
    <div className="flex min-h-full flex-col bg-kumo-canvas">
      <AppPageHeader
        title="Traffic"
        description="Bucketed uplink and downlink volume from node traffic reports, with a per-user breakdown."
        actions={
          <Button
            variant="secondary"
            icon={ArrowClockwiseIcon}
            disabled={totalQuery.isFetching || userQuery.isFetching}
            onClick={() => {
              setNowAnchor(new Date());
              setRefreshGeneration((value) => value + 1);
            }}
          >
            Refresh
          </Button>
        }
      />
      <main className="w-full grow bg-kumo-canvas">
        <div className="mx-auto flex w-full max-w-[1400px] flex-col gap-4 px-6 pb-8 md:px-8 lg:px-10">
          <Collapsible.Root open={filterOpen} onOpenChange={setFilterOpen}>
            <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
              <div className="flex min-w-0 flex-wrap items-center gap-2">
                <Tabs
                  size="sm"
                  aria-label="Metering"
                  tabs={[
                    { value: "billable", label: "Billable" },
                    { value: "raw", label: "Metered" }
                  ]}
                  value={filters.metric}
                  onValueChange={(value) =>
                    writeParams({ ...form.getValues(), metric: value === "raw" ? "raw" : "billable" })
                  }
                />
                <Tabs
                  size="sm"
                  aria-label="Granularity"
                  tabs={
                    hourlyAvailable
                      ? [
                          { value: "hour", label: "Hourly" },
                          { value: "day", label: "Daily" }
                        ]
                      : [{ value: "day", label: "Daily" }]
                  }
                  value={bucket}
                  onValueChange={(value) =>
                    writeParams({ ...form.getValues(), bucket: value === "hour" ? "hour" : "day" })
                  }
                />
                {!hourlyAvailable ? (
                  <span className="text-xs text-kumo-subtle">Hourly buckets need a range of 7 days or less.</span>
                ) : null}
              </div>
              <div className="flex shrink-0 flex-wrap items-center gap-2">
                <Popover>
                  <Popover.Trigger render={<Button variant="secondary" icon={CalendarBlankIcon} />}>
                    {timeRange.label}
                  </Popover.Trigger>
                  <Popover.Content>
                    <Popover.Title>Time range</Popover.Title>
                    <div className="mt-3 flex flex-wrap gap-2">
                      {rangePresets.map(([value, label]) => (
                        <Button
                          key={value}
                          variant={formValues.range === value ? "primary" : "secondary"}
                          size="sm"
                          onClick={() => setRangePreset(value)}
                        >
                          {label}
                        </Button>
                      ))}
                    </div>
                    <div className="mt-4">
                      <DatePicker
                        mode="range"
                        selected={draftRange}
                        onChange={(range) => {
                          setDraftRange(range ?? { from: undefined });
                          form.setValue("range", "custom");
                        }}
                        numberOfMonths={1}
                      />
                    </div>
                    <div className="mt-3 flex justify-end gap-2">
                      <Button
                        variant="secondary"
                        size="sm"
                        onClick={() => setDraftRange(dateRangeFromParams(filters, startParam, endParam, nowAnchor))}
                      >
                        Reset
                      </Button>
                      <Popover.Close render={<Button variant="primary" size="sm" onClick={applyCustomRange} />}>
                        Apply
                      </Popover.Close>
                    </div>
                  </Popover.Content>
                </Popover>

                <Collapsible.Trigger render={<Button type="button" variant="secondary" icon={FunnelIcon} />}>
                  Filter
                  {activeFilterCount > 0 ? (
                    <Badge variant="secondary" className="ml-1.5">{activeFilterCount}</Badge>
                  ) : null}
                </Collapsible.Trigger>
              </div>
            </div>

            <Collapsible.Panel className="mt-3 rounded-lg bg-kumo-tint p-3">
              <div className="grid gap-3 md:grid-cols-2">
                <Combobox
                  label="Node"
                  value={formValues.node}
                  onValueChange={(value) => form.setValue("node", (value as string | null) ?? "all")}
                  items={nodeChoices}
                >
                  <Combobox.TriggerValue placeholder="All nodes">
                    {(value) => (value === "all" ? "All nodes" : value)}
                  </Combobox.TriggerValue>
                  <Combobox.Content>
                    <Combobox.Input placeholder="Search nodes" />
                    <Combobox.Empty />
                    <Combobox.List>
                      {(item: string) => (
                        <Combobox.Item key={item} value={item}>
                          {item === "all" ? "All nodes" : item}
                        </Combobox.Item>
                      )}
                    </Combobox.List>
                  </Combobox.Content>
                </Combobox>

                <Combobox
                  label="User"
                  value={formValues.user}
                  onValueChange={(value) => form.setValue("user", (value as string | null) ?? "all")}
                  items={userChoices}
                >
                  <Combobox.TriggerValue placeholder="All users">
                    {(value) => (value === "all" ? "All users" : value)}
                  </Combobox.TriggerValue>
                  <Combobox.Content>
                    <Combobox.Input placeholder="Search users" />
                    <Combobox.Empty />
                    <Combobox.List>
                      {(item: string) => (
                        <Combobox.Item key={item} value={item}>
                          {item === "all" ? "All users" : item}
                        </Combobox.Item>
                      )}
                    </Combobox.List>
                  </Combobox.Content>
                </Combobox>
              </div>
              <div className="mt-3 flex justify-end gap-2">
                <Button variant="secondary" size="sm" onClick={clearPanelFilters}>
                  Reset
                </Button>
                <Button variant="primary" size="sm" onClick={applyPanelFilters}>
                  Apply
                </Button>
              </div>
            </Collapsible.Panel>
          </Collapsible.Root>

          <section aria-label="Traffic summary" className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
            <StatTile
              label={`Total ${metricLabel(filters.metric)}`}
              value={formatBytes(windowTotals.total)}
              detail={`${formatBytes(alternateTotals.total)} ${metricLabel(filters.metric === "raw" ? "billable" : "raw")}`}
              trend={totalTrend}
            />
            <StatTile
              label="Downlink"
              value={formatBytes(windowTotals.downlink)}
              detail={formatShare(windowTotals.downlink, windowTotals.total)}
            />
            <StatTile
              label="Uplink"
              value={formatBytes(windowTotals.uplink)}
              detail={formatShare(windowTotals.uplink, windowTotals.total)}
            />
            <StatTile
              label={bucket === "hour" ? "Busiest hour" : "Busiest day"}
              value={formatBytes(peak?.value ?? 0)}
              detail={peak && peak.value > 0 ? formatBucketStart(peak.bucketStart, bucket) : undefined}
            />
          </section>

          <LayerCard className="flex w-full flex-col">
            <LayerCard.Secondary className="flex flex-wrap items-center justify-between gap-2">
              <h2 className="text-base font-semibold text-kumo-default">Traffic over time</h2>
              <span className="text-sm text-kumo-subtle">
                {timeRange.label} - {bucket === "hour" ? "hourly" : "daily"} buckets
              </span>
            </LayerCard.Secondary>
            <LayerCard.Primary className="flex flex-col gap-3 p-4">
              {chartError ? (
                <Banner variant="error" title="Traffic series unavailable" description={chartError} />
              ) : chartEmpty ? (
                <Empty
                  size="sm"
                  title="No traffic in this range"
                  description="Widen the time range or clear the node and user filters."
                  className="min-h-52 justify-center"
                />
              ) : (
                <>
                  <TimeBarChart
                    series={chartSeries}
                    bucket={bucket}
                    height={260}
                    loading={totalQuery.isLoading}
                    valueFormat={formatBytes}
                    ariaDescription={`Stacked ${metricLabel(filters.metric)} traffic per ${bucket} between ${timeRange.label.toLowerCase()}, downlink and uplink. Per-user totals for the same window are listed in the table below.`}
                    onTimeRangeChange={selectChartRange}
                  />
                  <div className="flex flex-wrap items-center gap-x-6 gap-y-1">
                    <ChartLegend.SmallItem
                      name="Downlink"
                      color={colors[0]}
                      value={formatBytes(windowTotals.downlink)}
                    />
                    <ChartLegend.SmallItem name="Uplink" color={colors[1]} value={formatBytes(windowTotals.uplink)} />
                  </div>
                </>
              )}
            </LayerCard.Primary>
          </LayerCard>

          <section className="flex flex-col gap-3">
            <div className="flex flex-col gap-1 sm:flex-row sm:items-end sm:justify-between">
              <div>
                <h2 className="text-base font-semibold text-kumo-default">Traffic by user</h2>
                <p className="text-sm text-kumo-subtle">
                  {userRows.length > 0
                    ? `${userRows.length} user${userRows.length === 1 ? "" : "s"} with recorded traffic, ${metricLabel(filters.metric)} bytes`
                    : "No users with recorded traffic"}
                </p>
              </div>
            </div>

            {userQuery.data?.truncated ? (
              <Banner
                variant="alert"
                title={`Showing the top ${USER_SERIES_LIMIT} users`}
                description="Users beyond this ranking are excluded from the table but still counted in the chart and totals above."
              />
            ) : null}

            <TableCard>
              <Table className="min-w-[860px]">
                <Table.Header variant="compact">
                  <Table.Row>
                    <SortHead
                      label="User"
                      column="label"
                      sort={activeSort}
                      direction={activeDirection}
                      setSort={setSort}
                      sticky="left"
                    />
                    <Table.Head>Trend</Table.Head>
                    <SortHead label="Downlink" column="downlink" sort={activeSort} direction={activeDirection} setSort={setSort} />
                    <SortHead label="Uplink" column="uplink" sort={activeSort} direction={activeDirection} setSort={setSort} />
                    <SortHead label="Total" column="total" sort={activeSort} direction={activeDirection} setSort={setSort} />
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {userQuery.error ? (
                    <TableError colSpan={columns.length}>{userError}</TableError>
                  ) : userQuery.isLoading ? (
                    <TableLoading colSpan={columns.length} />
                  ) : table.getRowModel().rows.length > 0 ? (
                    table.getRowModel().rows.map((row) => (
                      <Table.Row key={row.id}>
                        {row.getVisibleCells().map((cell) => (
                          <Table.Cell key={cell.id} sticky={cell.column.id === "label" ? "left" : undefined}>
                            {flexRender(cell.column.columnDef.cell, cell.getContext())}
                          </Table.Cell>
                        ))}
                      </Table.Row>
                    ))
                  ) : (
                    <TableEmpty
                      colSpan={columns.length}
                      description="Traffic appears once nodes report usage for the selected window."
                    >
                      No user traffic in this range
                    </TableEmpty>
                  )}
                </Table.Body>
              </Table>
            </TableCard>
          </section>
        </div>
      </main>
    </div>
  );
}
