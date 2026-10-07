import { Fragment, useEffect, useMemo, useState, type ReactNode } from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import {
  createColumnHelper,
  flexRender,
  getCoreRowModel,
  useReactTable
} from "@tanstack/react-table";
import { useForm } from "react-hook-form";
import { useSearchParams } from "react-router-dom";
import type { DateRange } from "react-day-picker";
import { z } from "zod";
import {
  ArrowLeftIcon,
  CalendarBlankIcon,
  FunnelIcon,
  WarningCircleIcon
} from "@phosphor-icons/react";
import {
  Badge,
  Button,
  Collapsible,
  Combobox,
  DatePicker,
  Empty,
  Input,
  Loader,
  Popover,
  Table,
  Tabs
} from "@cloudflare/kumo";
import {
  endOfDay,
  format,
  formatDuration,
  intervalToDuration,
  isValid,
  parseISO,
  startOfDay,
  subDays,
  subHours
} from "date-fns";

import type {
  AdminNode,
  AdminUser,
  NetworkEvent,
  NetworkEventHostsResponse,
  NetworkEventSeriesResponse,
  NetworkEventsResponse,
  SeriesBucket,
  ServiceUsageGroup,
  ServiceUsageResponse
} from "../types";
import { useAdminApi } from "@/admin/api";
import { adminKeys, queryString } from "@/admin/query";
import { TableCard, TableEmpty, TableError, TableLoading } from "@/components/admin-table";
import { LogExpandButton, LogRowDetails } from "@/components/log-row-details";
import { LogWorkspace, LogFieldsToggle, LogActions, LogResults, manualLogQueryOptions } from "@/components/log-workspace";
import { AppPageHeader } from "@/components/app-page-header";
import { RankedBarList, type RankedBarRow } from "@/components/chart/ranked-bar-list";
import { TimeBarChart, type TimeSeries } from "@/components/chart/time-bar-chart";
import { formatBytes } from "../utils";

type RangePreset = "1h" | "24h" | "7d" | "30d" | "custom" | "all";

// Logs reads a server-owned union; unavailable legacy metadata stays null.

type ColumnMeta = {
  headClassName?: string;
  cellClassName?: string;
};

/**
 * The six filters every network-event endpoint reads with identical semantics.
 * One object feeds the table, the activity chart and the audit panel so the
 * three can never disagree about what is being shown.
 */
type EventScope = {
  search?: string;
  action?: string;
  node?: string;
  user?: string;
  start?: string;
  end?: string;
};

/**
 * The filters the connection endpoints read. Deliberately a subset of
 * `EventScope`: the stream carries no classified action, and connection_events
 * has no full-text index, so `action` and `search` would be silently ignored.
 * `host` is the drill-down the byte ranking hands back.
 */
const filterSchema = z.object({
  search: z.string(),
  action: z.string(),
  node: z.string(),
  user: z.string(),
  range: z.enum(["1h", "24h", "7d", "30d", "custom", "all"])
});

type FilterValues = z.infer<typeof filterSchema>;

const defaultFilters: FilterValues = {
  search: "",
  action: "all",
  node: "all",
  user: "all",
  range: "24h"
};


const HOUR_MS = 60 * 60 * 1000;
/** Span at or below which the server derives hour buckets when `bucket` is absent. */
const DERIVED_HOUR_SPAN_MS = 48 * HOUR_MS;
/**
 * 168 hourly bars is the practical ceiling in a full-width card, so hourly is
 * withdrawn past a week. The server independently rejects hour buckets at eight
 * days, which makes this a usability limit rather than the safety limit.
 */
const HOUR_BUCKET_MAX_SPAN_MS = 7 * 24 * HOUR_MS;
/** Services requested from the server; anything beyond lands in its `other` row. */
const AUDIT_SERVICE_ROWS = 10;
/** Hosts requested for a service drill-down. */
const AUDIT_HOST_ROWS = 50;
/** Destinations requested for the connection-stream byte ranking. */
/** Aggregated connection rows shown when drilling into one destination. */
const columnHelper = createColumnHelper<NetworkEvent>();

function validRange(value: string | null): RangePreset {
  if (value === "1h" || value === "24h" || value === "7d" || value === "30d" || value === "custom" || value === "all") {
    return value;
  }
  return "24h";
}

function parseDateParam(value: string | null): Date | null {
  if (!value) return null;
  const date = parseISO(value);
  return isValid(date) ? date : null;
}

function filtersFromSearchParams(params: URLSearchParams): FilterValues {
  return {
    search: params.get("search") ?? "",
    action: params.get("action") ?? "all",
    node: params.get("node") ?? "all",
    user: params.get("user") ?? "all",
    range: validRange(params.get("range"))
  };
}

function resolveTimeRange(filters: FilterValues, startParam: string | null, endParam: string | null, now: Date) {
  if (filters.range === "all") {
    return { start: undefined, end: undefined, label: "All time" };
  }
  if (filters.range === "custom") {
    const start = parseDateParam(startParam);
    const end = parseDateParam(endParam);
    if (!start || !end) {
      return { start: undefined, end: undefined, label: "Custom range" };
    }
    return { start: start.toISOString(), end: end.toISOString(), label: `${format(start, "MMM d")} - ${format(end, "MMM d")}` };
  }
  if (filters.range === "1h") {
    return { start: subHours(now, 1).toISOString(), end: now.toISOString(), label: "Last hour" };
  }
  if (filters.range === "7d") {
    return { start: subDays(now, 7).toISOString(), end: now.toISOString(), label: "Last 7 days" };
  }
  if (filters.range === "30d") {
    return { start: subDays(now, 30).toISOString(), end: now.toISOString(), label: "Last 30 days" };
  }
  return { start: subHours(now, 24).toISOString(), end: now.toISOString(), label: "Last 24 hours" };
}

function dateRangeFromParams(filters: FilterValues, startParam: string | null, endParam: string | null, now: Date): DateRange {
  const resolved = resolveTimeRange(filters, startParam, endParam, now);
  const start = resolved.start ? parseDateParam(resolved.start) : subHours(now, 24);
  const end = resolved.end ? parseDateParam(resolved.end) : now;
  return { from: start ?? subHours(now, 24), to: end ?? now };
}

/** Milliseconds covered by a resolved range, or null when the range is unbounded. */
export function seriesSpanMillis(start?: string, end?: string): number | null {
  if (!start || !end) return null;
  const from = Date.parse(start);
  const to = Date.parse(end);
  if (!Number.isFinite(from) || !Number.isFinite(to) || to <= from) return null;
  return to - from;
}

/**
 * Granularity actually sent to the server. The URL keeps the operator's choice,
 * but an unbounded or week-plus range can only be charted by day, so the choice
 * is clamped here instead of being answered with a 422.
 */
export function resolveSeriesBucket(
  requested: string | null,
  spanMs: number | null
): { bucket: SeriesBucket; hourAllowed: boolean } {
  const hourAllowed = spanMs !== null && spanMs <= HOUR_BUCKET_MAX_SPAN_MS;
  if (!hourAllowed) return { bucket: "day", hourAllowed };
  if (requested === "hour" || requested === "day") return { bucket: requested, hourAllowed };
  return { bucket: spanMs <= DERIVED_HOUR_SPAN_MS ? "hour" : "day", hourAllowed };
}

/**
 * Day buckets are cut at local midnight. The server adds this offset to UTC to
 * reach local time, which is the opposite sign of the JS convention. Hour
 * buckets stay UTC-aligned and send nothing.
 */
export function bucketOffsetMinutes(bucket: SeriesBucket, now = new Date()): number {
  return bucket === "day" ? -now.getTimezoneOffset() : 0;
}

function validBreakdown(value: string | null): ServiceUsageGroup {
  return value === "category" ? "category" : "service";
}

/**
 * Ranking dimension for the connection-stream host list. The server reads it
 * through the shared `group` whitelist and answers 422 for anything else, so the
 * URL is narrowed to the two values it accepts before a request is built.
 */
function formatEventTime(value: string): string {
  const date = parseDateParam(value);
  return date ? format(date, "MMM d, HH:mm") : "n/a";
}

function formatCount(value: number): string {
  return value.toLocaleString();
}

/** Units the summed session time is rendered in, largest first. */
const DURATION_UNITS = ["years", "months", "days", "hours", "minutes", "seconds"] as const;

/**
 * Summed session time, compact enough for a table cell. date-fns owns both the
 * calendar arithmetic and the wording; only the choice of the two largest
 * non-zero units is made here, so a busy bucket reads "3 hours 12 minutes"
 * rather than a six-term sentence.
 */
export function formatDurationMs(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return "0 seconds";
  if (value < 1000) return "<1 second";
  const duration = intervalToDuration({ start: 0, end: Math.round(value) });
  const largest = DURATION_UNITS.findIndex((unit) => (duration[unit] ?? 0) > 0);
  if (largest < 0) return "<1 second";
  return formatDuration(duration, {
    format: [...DURATION_UNITS].slice(largest, largest + 2),
    delimiter: " ",
    zero: false
  });
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "Request failed.";
}

function sourceLabel(source: string): string {
  if (source === "publicsuffix") return "Public suffix";
  if (source === "ip") return "IP literal";
  if (!source) return "Unknown";
  return source.charAt(0).toUpperCase() + source.slice(1);
}

function eventDestination(event: NetworkEvent): string {
  if (!event.target_host) return "n/a";
  return event.target_port ? `${event.target_host}:${event.target_port}` : event.target_host;
}

function columnClass(column: { columnDef: { meta?: unknown } }, key: keyof ColumnMeta) {
  return ((column.columnDef.meta as ColumnMeta | undefined)?.[key] ?? "") as string;
}

function Panel({ children, className = "" }: { children: ReactNode; className?: string }) {
  return (
    <div className={`flex flex-col rounded-lg border border-kumo-line bg-kumo-base ${className}`}>{children}</div>
  );
}

function PanelHeader({ children }: { children: ReactNode }) {
  return (
    <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2 border-b border-kumo-line px-4 py-3">
      {children}
    </div>
  );
}

/** Panel-shaped counterpart to `TableError`: a failed query never reads as empty. */
function PanelError({ children }: { children: string }) {
  return (
    <div className="flex min-h-36 items-center justify-center gap-2 p-4 text-sm text-kumo-danger">
      <WarningCircleIcon className="size-4 shrink-0" aria-hidden="true" />
      {children}
    </div>
  );
}

function PanelNotice({ title, description }: { title: string; description: string }) {
  return (
    <div className="flex min-h-36 items-center justify-center p-4">
      <Empty size="sm" title={title} description={description} />
    </div>
  );
}

/**
 * Connections over time, bucketed by the server.
 *
 * Every bucket boundary, zero-filled gap and ordering decision belongs to
 * `/network-events/series`; this component plots the points it is handed. The
 * previous client-side version aggregated only the visible table page, so
 * paging changed the chart — no client-side variant can fix that.
 */
export function ActivityPanel({
  scope,
  scopeKey,
  bucket,
  hourAllowed,
  onBucketChange,
  onTimeRangeChange
}: {
  scope: EventScope;
  scopeKey: object;
  bucket: SeriesBucket;
  hourAllowed: boolean;
  onBucketChange: (bucket: SeriesBucket) => void;
  onTimeRangeChange: (fromMs: number, toMs: number) => void;
}) {
  const { request } = useAdminApi();
  const offsetMinutes = bucketOffsetMinutes(bucket);
  const bounded = Boolean(scope.start && scope.end);
  const path = "/api/admin/network-events/series" + queryString({
    ...scope,
    bucket,
    offset_minutes: offsetMinutes,
    group: "total"
  });
  const query = useQuery({
    ...manualLogQueryOptions,
    queryKey: adminKeys.networkEventSeries({ ...scopeKey, bucket, offsetMinutes }),
    queryFn: ({ signal }) => request<NetworkEventSeriesResponse>(path, { signal }),
    placeholderData: (previous) => previous,
    enabled: bounded
  });

  const series = useMemo<TimeSeries[]>(() => {
    const total = query.data?.series[0];
    if (!total) return [];
    return [{
      key: total.key,
      label: total.label,
      points: total.points.map((point) => [Date.parse(point.bucket_start), point.count] as [number, number])
    }];
  }, [query.data]);

  const connections = query.data?.series[0]?.total ?? 0;
  const granularity = bucket === "hour" ? "hour" : "day";

  return (
    <Panel>
      <PanelHeader>
        <div>
          <h3 className="text-sm font-semibold text-kumo-default">Connection activity</h3>
          <p className="text-sm text-kumo-subtle">
            {bounded
              ? `${formatCount(connections)} connections in this range, bucketed by ${granularity}`
              : "Connections over time across the current filters"}
          </p>
        </div>
        <div className="flex items-center gap-1" role="group" aria-label="Chart granularity">
          <Button
            size="sm"
            variant={bucket === "hour" ? "primary" : "secondary"}
            aria-pressed={bucket === "hour"}
            disabled={!hourAllowed}
            title={hourAllowed ? undefined : "Hourly buckets cover at most 7 days"}
            onClick={() => onBucketChange("hour")}
          >
            Hourly
          </Button>
          <Button
            size="sm"
            variant={bucket === "day" ? "primary" : "secondary"}
            aria-pressed={bucket === "day"}
            onClick={() => onBucketChange("day")}
          >
            Daily
          </Button>
        </div>
      </PanelHeader>

      {!bounded ? (
        <PanelNotice
          title="Activity needs a bounded time range"
          description="Pick a preset or a custom range to chart connections over time."
        />
      ) : query.error ? (
        <PanelError>{errorMessage(query.error)}</PanelError>
      ) : (
        <div className="px-2 pt-2">
          <TimeBarChart
            series={series}
            bucket={bucket}
            height={150}
            loading={query.isLoading}
            valueFormat={formatCount}
            yAxisName="Connections"
            ariaDescription={`Connections per ${granularity} for the selected filters, oldest bucket first.`}
            onTimeRangeChange={onTimeRangeChange}
          />
        </div>
      )}

    </Panel>
  );
}

/**
 * Which services the filtered connections went to.
 *
 * This is a connection count, never a volume: `log_events` carries no byte
 * columns and traffic deltas carry no host, so bytes cannot be attributed to a
 * destination at all. Every label here says "connections" on purpose.
 */
export function ServiceAuditPanel({
  scope,
  scopeKey,
  breakdown,
  onBreakdownChange,
  service,
  onServiceChange
}: {
  scope: EventScope;
  scopeKey: object;
  breakdown: ServiceUsageGroup;
  onBreakdownChange: (breakdown: ServiceUsageGroup) => void;
  service: string;
  onServiceChange: (service: string) => void;
}) {
  const { request } = useAdminApi();
  const servicesPath = "/api/admin/network-events/services" + queryString({
    ...scope,
    group: breakdown,
    limit: AUDIT_SERVICE_ROWS
  });
  const servicesQuery = useQuery({
    ...manualLogQueryOptions,
    queryKey: adminKeys.networkEventServices({ ...scopeKey, group: breakdown }),
    queryFn: ({ signal }) => request<ServiceUsageResponse>(servicesPath, { signal }),
    placeholderData: (previous) => previous
  });

  const hostsPath = "/api/admin/network-events/hosts" + queryString({
    ...scope,
    service: service || undefined,
    limit: AUDIT_HOST_ROWS
  });
  const hostsQuery = useQuery({
    ...manualLogQueryOptions,
    queryKey: adminKeys.networkEventHosts({ ...scopeKey, service }),
    queryFn: ({ signal }) => request<NetworkEventHostsResponse>(hostsPath, { signal }),
    placeholderData: (previous) => previous,
    enabled: service !== ""
  });

  const usage = servicesQuery.data;
  const rows = useMemo<RankedBarRow[]>(
    () => (usage?.rows ?? []).map((row) => ({
      key: row.key,
      label: row.label,
      value: row.connections,
      secondary: `${formatCount(row.hosts)} ${row.hosts === 1 ? "host" : "hosts"}`
    })),
    [usage?.rows]
  );
  const other = usage?.other;
  const serviceLabel = usage?.rows.find((row) => row.key === service)?.label ?? service;
  const hosts = hostsQuery.data?.hosts ?? [];

  return (
    <Panel>
      <PanelHeader>
        <div>
          <h3 className="text-sm font-semibold text-kumo-default">
            {service ? `Hosts in ${serviceLabel}` : "Network activity audit"}
          </h3>
          <p className="text-sm text-kumo-subtle">
            {service
              ? "Destination hosts classified into this service, ranked by connections."
              : "Connections per destination service. Log events carry no byte counts, so this never measures volume."}
          </p>
        </div>
        <div className="flex items-center gap-1">
          {service ? (
            <Button size="sm" variant="secondary" icon={ArrowLeftIcon} onClick={() => onServiceChange("")}>
              All services
            </Button>
          ) : (
            <div className="flex items-center gap-1" role="group" aria-label="Audit breakdown">
              <Button
                size="sm"
                variant={breakdown === "service" ? "primary" : "secondary"}
                aria-pressed={breakdown === "service"}
                onClick={() => onBreakdownChange("service")}
              >
                Services
              </Button>
              <Button
                size="sm"
                variant={breakdown === "category" ? "primary" : "secondary"}
                aria-pressed={breakdown === "category"}
                onClick={() => onBreakdownChange("category")}
              >
                Categories
              </Button>
            </div>
          )}
        </div>
      </PanelHeader>

      {service ? (
        hostsQuery.error ? (
          <PanelError>{errorMessage(hostsQuery.error)}</PanelError>
        ) : hostsQuery.isLoading ? (
          <div className="flex min-h-36 items-center justify-center"><Loader size={20} /></div>
        ) : hosts.length === 0 ? (
          <PanelNotice
            title="No hosts in this service"
            description="Adjust the filters or time range to see more destinations."
          />
        ) : (
          <>
            <div className="overflow-x-auto overscroll-x-contain">
              <Table className="min-w-[720px] table-fixed">
                <Table.Header variant="compact">
                  <Table.Row>
                    <Table.Head className="w-72">Host</Table.Head>
                    <Table.Head className="w-36">Category</Table.Head>
                    <Table.Head className="w-32">Match</Table.Head>
                    <Table.Head className="w-28">Connections</Table.Head>
                    <Table.Head className="w-40">Last seen</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {hosts.map((host) => (
                    <Table.Row key={host.host}>
                      <Table.Cell className="w-72">
                        <span className="block truncate text-kumo-default" title={host.host}>{host.host}</span>
                      </Table.Cell>
                      <Table.Cell className="w-36">
                        <span className="block truncate text-kumo-subtle" title={host.category}>{host.category || "n/a"}</span>
                      </Table.Cell>
                      <Table.Cell className="w-32">
                        <Badge variant="secondary">{sourceLabel(host.source)}</Badge>
                      </Table.Cell>
                      <Table.Cell className="w-28">
                        <span className="whitespace-nowrap text-kumo-default tabular-nums">{formatCount(host.connections)}</span>
                      </Table.Cell>
                      <Table.Cell className="w-40">
                        <span className="whitespace-nowrap text-kumo-subtle">{formatEventTime(host.last_seen)}</span>
                      </Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table>
            </div>
            <p className="border-t border-kumo-line px-4 py-2 text-xs text-kumo-subtle">
              Showing {formatCount(hosts.length)} of {formatCount(hostsQuery.data?.total ?? hosts.length)} hosts.
            </p>
          </>
        )
      ) : servicesQuery.error ? (
        <PanelError>{errorMessage(servicesQuery.error)}</PanelError>
      ) : (
        <>
          <div className="px-2 py-1.5">
            <RankedBarList
              rows={rows}
              total={usage?.total_connections ?? 0}
              valueFormat={formatCount}
              maxRows={AUDIT_SERVICE_ROWS}
              loading={servicesQuery.isLoading}
              emptyLabel="No classified connections in this range"
              onSelect={breakdown === "service" ? onServiceChange : undefined}
            />
          </div>
          {usage ? (
            <p className="border-t border-kumo-line px-4 py-2 text-xs text-kumo-subtle">
              {formatCount(usage.total_connections)} connections across {formatCount(usage.total_hosts)} hosts
              {other && other.connections > 0
                ? `, of which ${formatCount(other.connections)} fall outside the top ${AUDIT_SERVICE_ROWS}`
                : ""}
              . Catalog {usage.catalog_version}.
              {usage.truncated ? " This range has more distinct hosts than one breakdown can classify, so the ranking is partial." : ""}
            </p>
          ) : null}
        </>
      )}
    </Panel>
  );
}

/**
 * Estimated volume plus the coverage figure that qualifies it.
 *
 * The two are rendered together on purpose. Bytes from the connection stream are
 * a best-effort estimate — sing-box drops silently when a subscriber buffer
 * fills, evicts its closed-connection ring, and resets connection ids on restart
 * — so a total shown without its coverage would read as a ledger. It is not one:
 * per-user billing stays on the V2Ray counters behind /traffic/series.
 *
 * Coverage is a property of the node's stream rather than of the filter.
 * `connection_reports` has no user and no host column, so a user or host filter
 * narrows the totals above it and leaves the coverage node-wide. Saying so is
 * cheaper than letting an operator infer the wrong thing.
 */
export function NetworkEventsPage() {
  const [expandedEvent, setExpandedEvent] = useState<string | null>(null);
  const { request } = useAdminApi();
  const [searchParams, setSearchParams] = useSearchParams();
  const [nowAnchor, setNowAnchor] = useState(() => new Date());
  const [refreshGeneration, setRefreshGeneration] = useState(0);
  const [filterOpen, setFilterOpen] = useState(false);
  const [rangeOpen, setRangeOpen] = useState(false);
  const filters = useMemo(() => filtersFromSearchParams(searchParams), [searchParams]);
  const startParam = searchParams.get("start");
  const endParam = searchParams.get("end");
  const perPage = Math.max(1, Math.min(Number(searchParams.get("limit") ?? 25) || 25, 100));
  const offset = Math.max(0, Number(searchParams.get("offset") ?? 0) || 0);
  const timeRange = useMemo(() => resolveTimeRange(filters, startParam, endParam, nowAnchor), [endParam, filters, nowAnchor, startParam]);
  const [draftRange, setDraftRange] = useState<DateRange>(() => dateRangeFromParams(filters, startParam, endParam, nowAnchor));
  const { bucket, hourAllowed } = resolveSeriesBucket(
    searchParams.get("bucket"),
    seriesSpanMillis(timeRange.start, timeRange.end)
  );
  const breakdown = validBreakdown(searchParams.get("breakdown"));
  const service = searchParams.get("service") ?? "";
  const view = searchParams.get("view") === "services" ? "services" : "logs";
  useEffect(() => {
    if (searchParams.get("view") === "connections") {
      const next = new URLSearchParams(searchParams);
      next.delete("view"); next.delete("chost"); next.delete("csort");
      setSearchParams(next, { replace: true });
    }
  }, [searchParams, setSearchParams]);

  const form = useForm<FilterValues>({
    resolver: zodResolver(filterSchema),
    values: filters
  });
  const formValues = form.watch();

  useEffect(() => {
    setDraftRange(dateRangeFromParams(filters, startParam, endParam, nowAnchor));
  }, [endParam, filters, nowAnchor, startParam]);

  function writeParams(values: FilterValues, nextLimit = perPage, nextOffset = 0, nextStart = startParam, nextEnd = endParam) {
    if (values.range !== "custom") setNowAnchor(new Date());
    const next = new URLSearchParams();
    if (values.search.trim()) next.set("search", values.search.trim());
    if (values.action !== "all") next.set("action", values.action);
    if (values.node !== "all") next.set("node", values.node);
    if (values.user !== "all") next.set("user", values.user);
    if (values.range !== "24h") next.set("range", values.range);
    if (values.range === "custom") {
      if (nextStart) next.set("start", nextStart);
      if (nextEnd) next.set("end", nextEnd);
    }
    // View preferences survive a filter change; only an explicit reset drops them.
    for (const key of ["bucket", "breakdown", "service", "chost", "csort", "view"] as const) {
      const carried = searchParams.get(key);
      if (carried) next.set(key, carried);
    }
    next.set("limit", String(nextLimit));
    if (nextOffset > 0) next.set("offset", String(nextOffset));
    setSearchParams(next);
  }

  function setViewParam(key: "bucket" | "breakdown" | "service" | "chost" | "csort" | "view", value: string) {
    const next = new URLSearchParams(searchParams);
    if (value) next.set(key, value);
    else next.delete(key);
    setSearchParams(next);
  }

  function applyFilters(values: FilterValues) {
    writeParams(values, perPage, 0);
  }

  function clearFilters() {
    form.reset(defaultFilters);
    setSearchParams(new URLSearchParams({ limit: String(perPage) }));
  }

  function setRangePreset(value: RangePreset) {
    form.setValue("range", value);
    if (value !== "custom") {
      writeParams({ ...form.getValues(), range: value }, perPage, 0, null, null);
    }
  }

  function applyCustomRange() {
    const from = draftRange.from ? startOfDay(draftRange.from).toISOString() : null;
    const to = draftRange.to ? endOfDay(draftRange.to).toISOString() : draftRange.from ? endOfDay(draftRange.from).toISOString() : null;
    writeParams({ ...form.getValues(), range: "custom" }, perPage, 0, from, to);
  }

  // Dragging across the chart narrows every query on the page, not just the chart.
  function applyChartRange(fromMs: number, toMs: number) {
    if (!Number.isFinite(fromMs) || !Number.isFinite(toMs) || toMs <= fromMs) return;
    writeParams(
      { ...form.getValues(), range: "custom" },
      perPage,
      0,
      new Date(fromMs).toISOString(),
      new Date(toMs).toISOString()
    );
  }

  const scope = useMemo<EventScope>(() => ({
    search: filters.search.trim() || undefined,
    action: filters.action === "all" ? undefined : filters.action,
    node: filters.node === "all" ? undefined : filters.node,
    user: filters.user === "all" ? undefined : filters.user,
    start: timeRange.start,
    end: timeRange.end
  }), [filters, timeRange.end, timeRange.start]);

  // Preset ranges deliberately key on the preset instead of their exact
  // millisecond timestamps. This lets a quick route revisit use TanStack's
  // short-lived cache; once stale, the current queryFn still fetches a fresh
  // time window. Custom ranges retain their exact boundaries in the key.
  const scopeKey = useMemo(() => ({
    search: filters.search.trim(),
    action: filters.action,
    node: filters.node,
    user: filters.user,
    range: filters.range,
    start: filters.range === "custom" ? timeRange.start : undefined,
    end: filters.range === "custom" ? timeRange.end : undefined,
    refreshGeneration
  }), [filters, refreshGeneration, timeRange.end, timeRange.start]);

  const eventsQuery = useInfiniteQuery({
    ...manualLogQueryOptions,
    queryKey: [...adminKeys.networkEvents({ ...scopeKey, limit: perPage, offset }), "infinite"],
    initialPageParam: offset,
    queryFn: ({ signal, pageParam }) => request<NetworkEventsResponse>("/api/admin/network-events" + queryString({ ...scope, limit: perPage, offset: pageParam }), { signal }),
    getNextPageParam: (last, _pages, previousOffset) => {
      const next = previousOffset + last.events.length;
      return last.events.length && next < last.total ? next : undefined;
    }
  });
  const nodesQuery = useQuery({
    ...manualLogQueryOptions,
    queryKey: adminKeys.nodes,
    queryFn: ({ signal }) => request<AdminNode[]>("/api/admin/nodes", { signal })
  });
  const usersQuery = useQuery({
    ...manualLogQueryOptions,
    queryKey: adminKeys.users(false),
    queryFn: ({ signal }) => request<AdminUser[]>("/api/admin/users", { signal })
  });

  const events = useMemo(() => eventsQuery.data?.pages.flatMap((page) => page.events) ?? [], [eventsQuery.data]);
  const total = eventsQuery.data?.pages[0]?.total ?? 0;
  const nodeChoices = useMemo(() => ["all", ...(nodesQuery.data ?? []).map((node) => node.name)], [nodesQuery.data]);
  const userChoices = useMemo(() => ["all", ...(usersQuery.data ?? []).map((user) => user.name)], [usersQuery.data]);
  const activeFilterCount = [
    filters.search.trim() !== "",
    filters.action !== "all",
    filters.node !== "all",
    filters.user !== "all",
    filters.range !== defaultFilters.range
  ].filter(Boolean).length;

  function applyPanelFilters() {
    void form.handleSubmit((values) => {
      applyFilters(values);
      setFilterOpen(false);
    })();
  }

  function clearPanelFilters() {
    clearFilters();
    setFilterOpen(false);
  }

  const columns = useMemo(() => [
    columnHelper.display({ id: "details", header: "Details", cell: () => null }),
    columnHelper.accessor("window_end", {
      header: "Time",
      cell: (info) => (
        <div
          className="flex min-w-0 items-baseline justify-between gap-3 whitespace-nowrap"
          title={info.row.original.created_at}
        >
          <span className="font-mono text-xs text-kumo-default">{parseDateParam(info.getValue()) ? format(parseDateParam(info.getValue())!, "yyyy-MM-dd HH:mm:ss.SSS") : "—"}</span>
        </div>
      ),
      meta: { headClassName: "w-56", cellClassName: "w-56" }
    }),
    columnHelper.accessor("user_name", {
      header: "User",
      cell: (info) => <span className="block truncate text-kumo-default" title={info.getValue()}>{info.getValue() || "—"}</span>,
      meta: { headClassName: "w-40", cellClassName: "w-40" }
    }),
    columnHelper.accessor("node_name", {
      header: "Node",
      cell: (info) => <span className="block truncate text-kumo-subtle" title={info.getValue()}>{info.getValue() || "—"}</span>,
      meta: { headClassName: "w-28", cellClassName: "w-28" }
    }),
    columnHelper.accessor("source_ip", {
      header: "Source IP",
      cell: (info) => <span className="block truncate font-mono text-sm text-kumo-subtle" title={info.getValue()}>{info.getValue() || "—"}</span>,
      meta: { headClassName: "w-36", cellClassName: "w-36" }
    }),
    columnHelper.display({
      id: "destination",
      header: "Destination",
      cell: (info) => <span className="block max-w-64 truncate text-kumo-default" title={eventDestination(info.row.original)}>{eventDestination(info.row.original)}</span>,
      meta: { headClassName: "w-64", cellClassName: "w-64" }
    }),
    columnHelper.accessor("count", {
      header: "Count",
      cell: (info) => <span className="whitespace-nowrap text-kumo-subtle tabular-nums">{info.getValue()}</span>,
      meta: { headClassName: "w-20", cellClassName: "w-20" }
    }),
    columnHelper.accessor("auth_name", {
      header: "Auth",
      cell: (info) => <span className="block max-w-44 truncate text-kumo-subtle" title={info.getValue()}>{info.getValue() || "—"}</span>,
      meta: { headClassName: "w-44", cellClassName: "w-44" }
    }),
    columnHelper.accessor("network", { header: "Network", cell: (info) => info.getValue() || "—" }),
    columnHelper.accessor("uplink_bytes", { header: "Upload", cell: (info) => info.getValue() == null ? "—" : formatBytes(info.getValue()!) }),
    columnHelper.accessor("downlink_bytes", { header: "Download", cell: (info) => info.getValue() == null ? "—" : formatBytes(info.getValue()!) }),
    columnHelper.accessor("duration_ms", { header: "Duration", cell: (info) => info.getValue() == null || !info.row.original.connections_closed ? "—" : formatDurationMs(info.getValue()!) }),
    columnHelper.accessor("outbound", { header: "Outbound", cell: (info) => info.getValue() || "—" }),
    columnHelper.accessor("raw_message", {
      header: "Message",
      cell: (info) => <span className="block max-w-80 truncate text-kumo-subtle" title={info.getValue()}>{info.getValue() || "—"}</span>,
      meta: { headClassName: "w-80", cellClassName: "w-80" }
    })
  ], []);

  const table = useReactTable({
    data: events,
    columns,
    getCoreRowModel: getCoreRowModel(),
    getRowId: (row) => row.id ?? [row.node_name, row.auth_name, row.window_start, row.source_ip, row.target_host, row.target_port].join("|"),
    manualPagination: true,
    pageCount: Math.max(1, Math.ceil(total / perPage))
  });

  return (
    <div className="flex min-h-full min-w-0 flex-col bg-kumo-canvas">
      <AppPageHeader
        compact
        title="Network Events"
        description="Review connection records, users, nodes, and destinations."

      />
      <main className="w-full grow bg-kumo-canvas">
        <LogWorkspace>
        <div className="flex w-full min-w-0 flex-col gap-4">
          <section className="flex flex-col gap-4">
            <Tabs variant="underline" value={view} onValueChange={(value) => setViewParam("view", value === "logs" ? "" : value)} tabs={[{ value: "logs", label: "Logs" }, { value: "services", label: "Service activity" }]} />

            <Collapsible.Root open={filterOpen} onOpenChange={setFilterOpen}>
              <div className="bf-log-toolbar flex flex-wrap items-center gap-2">
                <LogFieldsToggle />
                <Collapsible.Trigger render={<Button type="button" variant="secondary" icon={FunnelIcon} />}>Add filter{activeFilterCount > 0 ? <Badge variant="secondary" className="ml-1.5">{activeFilterCount}</Badge> : null}</Collapsible.Trigger>
                <form
                  className="flex min-w-0 flex-1 gap-2"
                  onSubmit={form.handleSubmit(applyFilters)}
                >
                  <Input
                    placeholder="Search logs…"
                    aria-label="Search network events"
                    className="min-w-0 flex-1"
                    {...form.register("search")}
                  />
                  <Button type="submit" variant="secondary">
                    Search
                  </Button>
                </form>
                <div className="ml-auto flex shrink-0 items-center gap-2">
                  <Popover open={rangeOpen} onOpenChange={setRangeOpen}>
                    <Popover.Trigger
                      render={
                        <Button variant="secondary" icon={CalendarBlankIcon} />
                      }
                    >
                      {timeRange.label}
                    </Popover.Trigger>
                    <Popover.Content>
                      <Popover.Title>Time range</Popover.Title>
                      <div className="mt-3 flex flex-wrap gap-2">
                        {([
                          ["1h", "Last hour"],
                          ["24h", "Last 24 hours"],
                          ["7d", "Last 7 days"],
                          ["30d", "Last 30 days"],
                          ["all", "All time"]
                        ] as const).map(([value, label]) => (
                          <Button key={value} variant={formValues.range === value ? "primary" : "secondary"} size="sm" onClick={() => setRangePreset(value)}>
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
                        <Button variant="secondary" size="sm" onClick={() => setDraftRange(dateRangeFromParams(filters, startParam, endParam, nowAnchor))}>
                          Reset
                        </Button>
                        <Popover.Close render={<Button variant="primary" size="sm" onClick={applyCustomRange} />}>
                          Apply
                        </Popover.Close>
                      </div>
                    </Popover.Content>
                  </Popover>
                  <LogActions busy={eventsQuery.isFetching} refresh={() => { setExpandedEvent(null); setNowAnchor(new Date()); setRefreshGeneration((value) => value + 1); }} />


                </div>
              </div>

              <Collapsible.Panel className="rounded-lg bg-kumo-tint p-3">
                <div className="grid gap-3 md:grid-cols-3">
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

            {view === "logs" ? <>
            <ActivityPanel
              scope={scope}
              scopeKey={scopeKey}
              bucket={bucket}
              hourAllowed={hourAllowed}
              onBucketChange={(value) => setViewParam("bucket", value)}
              onTimeRangeChange={applyChartRange}
            />

            <TableCard key={JSON.stringify(scopeKey)} loadMore={{ hasMore: !!eventsQuery.hasNextPage && !eventsQuery.isFetchNextPageError, loading: eventsQuery.isFetching, fetch: () => { void eventsQuery.fetchNextPage({ cancelRefetch: false }); } }} tableId="network-events-unified" variant="log" className="bf-log-edge" widths={[36, 270, 120, 136, 180, 220, 80, 160, 100, 120, 120, 120, 160, { min: 320 }]}>
              <Table className="table-fixed">
                <Table.Header variant="compact">
                  {table.getHeaderGroups().map((headerGroup) => (
                    <Table.Row key={headerGroup.id}>
                      {headerGroup.headers.map((header) => (
                        <Table.Head
                          key={header.id}

                          className={columnClass(header.column, "headClassName")}
                        >
                          {header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}
                        </Table.Head>
                      ))}
                    </Table.Row>
                  ))}
                </Table.Header>
                <Table.Body>
                  {eventsQuery.error && !events.length ? (
                    <TableError colSpan={columns.length}>{errorMessage(eventsQuery.error)}</TableError>
                  ) : eventsQuery.isLoading ? (
                    <TableLoading colSpan={columns.length} />
                  ) : table.getRowModel().rows.length > 0 ? (
                    table.getRowModel().rows.map((row) => (
                      <Fragment key={row.id}><Table.Row>
                        {row.getVisibleCells().map((cell) => (
                          <Table.Cell
                            key={cell.id}

                            className={columnClass(cell.column, "cellClassName")}
                          >
                            {cell.column.id === "details" ? <LogExpandButton expanded={expandedEvent === row.id} onClick={() => setExpandedEvent(expandedEvent === row.id ? null : row.id)} label="network event" /> : flexRender(cell.column.columnDef.cell, cell.getContext())}
                          </Table.Cell>
                        ))}
                      </Table.Row>
                      {expandedEvent === row.id ? <LogRowDetails value={row.original} colSpan={columns.length} /> : null}
                      </Fragment>
                    ))
                  ) : (
                    <TableEmpty colSpan={columns.length} description="Adjust the filters or time range to see more events.">
                      No events match this filter
                    </TableEmpty>
                  )}
                </Table.Body>
              </Table>
            </TableCard>

            <LogResults loaded={events.length} total={total} loading={eventsQuery.isFetchingNextPage} error={eventsQuery.isFetchNextPageError} retry={() => { void eventsQuery.fetchNextPage(); }} />
            </> : null}
            {view === "services" ? <>
            <ServiceAuditPanel
              scope={scope}
              scopeKey={scopeKey}
              breakdown={breakdown}
              onBreakdownChange={(value) => setViewParam("breakdown", value === "service" ? "" : value)}
              service={service}
              onServiceChange={(value) => setViewParam("service", value)}
            />

            </> : null}
          </section>


        </div>
      </LogWorkspace>
      </main>
    </div>
  );
}
