import { useEffect, useMemo, useState, type ReactNode } from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import { useQuery } from "@tanstack/react-query";
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
  ArrowClockwiseIcon,
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
  Select,
  Table
} from "@cloudflare/kumo";
import {
  endOfDay,
  format,
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
import { AdminPagination, TableCard, TableEmpty, TableError, TableLoading } from "@/components/admin-table";
import { AppPageHeader } from "@/components/app-page-header";
import { RankedBarList, type RankedBarRow } from "@/components/chart/ranked-bar-list";
import { TimeBarChart, type TimeSeries } from "@/components/chart/time-bar-chart";
import { StatusBadge } from "@/components/status-badge";
import { formatRelativeTime } from "./operations-common";

type RangePreset = "1h" | "24h" | "7d" | "30d" | "custom" | "all";

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

const commonActions = ["connect", "outbound_connect", "invalid_connection", "accept", "reject"] as const;

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

function formatEventTime(value: string): string {
  const date = parseDateParam(value);
  return date ? format(date, "MMM d, HH:mm") : "n/a";
}

function formatCount(value: number): string {
  return value.toLocaleString();
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "Request failed.";
}

function actionLabel(action: string): string {
  if (action === "outbound_connect") return "Outbound";
  if (action === "invalid_connection") return "Invalid";
  if (!action) return "Unknown";
  const text = action.replace(/_/g, " ");
  return text.charAt(0).toUpperCase() + text.slice(1);
}

function actionTone(action: string): "success" | "warning" | "error" | "neutral" {
  const value = action.toLowerCase();
  if (value === "connect" || value === "outbound_connect" || value === "accept") {
    return "success";
  }
  if (value === "reject" || value === "block" || value === "blocked") {
    return "error";
  }
  if (value === "invalid_connection") {
    return "warning";
  }
  return "neutral";
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

  const actions = query.data?.actions ?? [];
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
            height={200}
            loading={query.isLoading}
            valueFormat={formatCount}
            yAxisName="Connections"
            ariaDescription={`Connections per ${granularity} for the selected filters, oldest bucket first.`}
            onTimeRangeChange={onTimeRangeChange}
          />
        </div>
      )}

      {actions.length > 0 ? (
        <div className="flex flex-wrap gap-x-6 gap-y-1 border-t border-kumo-line px-4 py-2">
          {actions.map((entry) => (
            <span key={entry.action} className="inline-flex items-center gap-1.5 whitespace-nowrap text-sm text-kumo-default">
              <StatusBadge tone={actionTone(entry.action)}>{actionLabel(entry.action)}</StatusBadge>
              <span className="font-semibold tabular-nums">{formatCount(entry.count)}</span>
            </span>
          ))}
        </div>
      ) : null}
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

export function NetworkEventsPage() {
  const { request } = useAdminApi();
  const [searchParams, setSearchParams] = useSearchParams();
  const [nowAnchor, setNowAnchor] = useState(() => new Date());
  const [refreshGeneration, setRefreshGeneration] = useState(0);
  const [filterOpen, setFilterOpen] = useState(false);
  const filters = useMemo(() => filtersFromSearchParams(searchParams), [searchParams]);
  const startParam = searchParams.get("start");
  const endParam = searchParams.get("end");
  const perPage = Math.max(1, Math.min(Number(searchParams.get("limit") ?? 25) || 25, 100));
  const offset = Math.max(0, Number(searchParams.get("offset") ?? 0) || 0);
  const page = Math.floor(offset / perPage) + 1;
  const timeRange = useMemo(() => resolveTimeRange(filters, startParam, endParam, nowAnchor), [endParam, filters, nowAnchor, startParam]);
  const [draftRange, setDraftRange] = useState<DateRange>(() => dateRangeFromParams(filters, startParam, endParam, nowAnchor));
  const { bucket, hourAllowed } = resolveSeriesBucket(
    searchParams.get("bucket"),
    seriesSpanMillis(timeRange.start, timeRange.end)
  );
  const breakdown = validBreakdown(searchParams.get("breakdown"));
  const service = searchParams.get("service") ?? "";

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
    for (const key of ["bucket", "breakdown", "service"] as const) {
      const carried = searchParams.get(key);
      if (carried) next.set(key, carried);
    }
    next.set("limit", String(nextLimit));
    if (nextOffset > 0) next.set("offset", String(nextOffset));
    setSearchParams(next);
  }

  function setViewParam(key: "bucket" | "breakdown" | "service", value: string) {
    const next = new URLSearchParams(searchParams);
    if (value) next.set(key, value);
    else next.delete(key);
    setSearchParams(next);
  }

  function setPage(value: number) {
    writeParams(filters, perPage, Math.max(0, (value - 1) * perPage));
  }

  function setPageSize(value: number) {
    writeParams(filters, value, 0);
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

  const path = "/api/admin/network-events" + queryString({ ...scope, limit: perPage, offset });
  const eventsQuery = useQuery({
    queryKey: adminKeys.networkEvents({ ...scopeKey, limit: perPage, offset }),
    queryFn: ({ signal }) => request<NetworkEventsResponse>(path, { signal }),
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

  const events = useMemo(() => eventsQuery.data?.events ?? [], [eventsQuery.data?.events]);
  const total = eventsQuery.data?.total ?? 0;
  const actionOptions = useMemo(() => {
    const values = new Set<string>(commonActions);
    for (const event of events) {
      if (event.action) values.add(event.action);
    }
    return [...values].sort();
  }, [events]);
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
    columnHelper.accessor("window_end", {
      header: "Time",
      cell: (info) => (
        <div
          className="flex min-w-0 items-baseline justify-between gap-3 whitespace-nowrap"
          title={info.row.original.created_at}
        >
          <span className="text-kumo-default">{formatEventTime(info.getValue())}</span>
          <span className="text-xs text-kumo-subtle">{formatRelativeTime(info.getValue())}</span>
        </div>
      ),
      meta: { headClassName: "w-56", cellClassName: "w-56" }
    }),
    columnHelper.accessor("action", {
      header: "Action",
      cell: (info) => (
        <StatusBadge tone={actionTone(info.getValue())}>{actionLabel(info.getValue())}</StatusBadge>
      ),
      meta: { headClassName: "w-36", cellClassName: "w-36" }
    }),
    columnHelper.accessor("user_name", {
      header: "User",
      cell: (info) => <span className="block truncate text-kumo-default" title={info.getValue()}>{info.getValue() || "n/a"}</span>,
      meta: { headClassName: "w-40", cellClassName: "w-40" }
    }),
    columnHelper.accessor("node_name", {
      header: "Node",
      cell: (info) => <span className="block truncate text-kumo-subtle" title={info.getValue()}>{info.getValue() || "n/a"}</span>,
      meta: { headClassName: "w-28", cellClassName: "w-28" }
    }),
    columnHelper.accessor("source_ip", {
      header: "Source IP",
      cell: (info) => <span className="block truncate font-mono text-sm text-kumo-subtle" title={info.getValue()}>{info.getValue() || "n/a"}</span>,
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
      cell: (info) => <span className="block max-w-44 truncate text-kumo-subtle" title={info.getValue()}>{info.getValue() || "n/a"}</span>,
      meta: { headClassName: "w-44", cellClassName: "w-44" }
    }),
    columnHelper.accessor("raw_message", {
      header: "Message",
      cell: (info) => <span className="block max-w-80 truncate text-kumo-subtle" title={info.getValue()}>{info.getValue() || "n/a"}</span>,
      meta: { headClassName: "w-80", cellClassName: "w-80" }
    })
  ], []);

  const table = useReactTable({
    data: events,
    columns,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    pageCount: Math.max(1, Math.ceil(total / perPage))
  });

  return (
    <div className="flex min-h-full flex-col bg-kumo-canvas">
      <AppPageHeader
        title="Network Events"
        description="Review parsed sing-box connection events, users, nodes, destinations, and raw log context."
        actions={
          <Button
            variant="secondary"
            icon={ArrowClockwiseIcon}
            disabled={eventsQuery.isFetching}
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
          <section className="flex flex-col gap-4">
            <div className="flex flex-col gap-1 sm:flex-row sm:items-end sm:justify-between">
              <div>
                <h2 className="text-base font-semibold text-kumo-default">Network events</h2>
                <p className="text-sm text-kumo-subtle">
                  {total > 0 ? `Showing ${offset + 1}-${Math.min(offset + perPage, total)} of ${total}` : "No events"}
                </p>
              </div>
              <p className="text-sm text-kumo-subtle">{timeRange.label}</p>
            </div>

            <Collapsible.Root open={filterOpen} onOpenChange={setFilterOpen}>
              <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
                <form
                  className="flex min-w-0 flex-1 gap-2"
                  onSubmit={form.handleSubmit(applyFilters)}
                >
                  <Input
                    placeholder="Search words or prefixes across user, node, IP, destination, action, or message"
                    aria-label="Search network events"
                    className="min-w-0 flex-1"
                    {...form.register("search")}
                  />
                  <Button type="submit" variant="secondary">
                    Search
                  </Button>
                </form>
                <div className="flex shrink-0 flex-wrap items-center gap-2">
                  <Popover>
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

                  <Collapsible.Trigger render={<Button type="button" variant="secondary" icon={FunnelIcon} />}>
                    Filter
                    {activeFilterCount > 0 ? (
                      <Badge variant="secondary" className="ml-1.5">{activeFilterCount}</Badge>
                    ) : null}
                  </Collapsible.Trigger>
                </div>
              </div>

              <Collapsible.Panel className="rounded-lg bg-kumo-tint p-3">
                <div className="grid gap-3 md:grid-cols-3">
                  <Select
                    label="Action"
                    value={formValues.action}
                    onValueChange={(value) => form.setValue("action", value ?? "all")}
                    items={[
                      { value: "all", label: "All actions" },
                      ...actionOptions.map((value) => ({ value, label: actionLabel(value) }))
                    ]}
                  />

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

            <ActivityPanel
              scope={scope}
              scopeKey={scopeKey}
              bucket={bucket}
              hourAllowed={hourAllowed}
              onBucketChange={(value) => setViewParam("bucket", value)}
              onTimeRangeChange={applyChartRange}
            />

            <ServiceAuditPanel
              scope={scope}
              scopeKey={scopeKey}
              breakdown={breakdown}
              onBreakdownChange={(value) => setViewParam("breakdown", value === "service" ? "" : value)}
              service={service}
              onServiceChange={(value) => setViewParam("service", value)}
            />

            <TableCard>
              <Table className="min-w-[1600px] table-fixed">
                <Table.Header variant="compact">
                  {table.getHeaderGroups().map((headerGroup) => (
                    <Table.Row key={headerGroup.id}>
                      {headerGroup.headers.map((header) => (
                        <Table.Head
                          key={header.id}
                          sticky={header.column.id === "window_end" ? "left" : undefined}
                          className={columnClass(header.column, "headClassName")}
                        >
                          {header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}
                        </Table.Head>
                      ))}
                    </Table.Row>
                  ))}
                </Table.Header>
                <Table.Body>
                  {eventsQuery.error ? (
                    <TableError colSpan={columns.length}>{errorMessage(eventsQuery.error)}</TableError>
                  ) : eventsQuery.isLoading ? (
                    <TableLoading colSpan={columns.length} />
                  ) : table.getRowModel().rows.length > 0 ? (
                    table.getRowModel().rows.map((row) => (
                      <Table.Row key={row.id}>
                        {row.getVisibleCells().map((cell) => (
                          <Table.Cell
                            key={cell.id}
                            sticky={cell.column.id === "window_end" ? "left" : undefined}
                            className={columnClass(cell.column, "cellClassName")}
                          >
                            {flexRender(cell.column.columnDef.cell, cell.getContext())}
                          </Table.Cell>
                        ))}
                      </Table.Row>
                    ))
                  ) : (
                    <TableEmpty colSpan={columns.length} description="Adjust the filters or time range to see more events.">
                      No events match this filter
                    </TableEmpty>
                  )}
                </Table.Body>
              </Table>
            </TableCard>

            <AdminPagination page={page} setPage={setPage} perPage={perPage} setPerPage={setPageSize} total={total} />
          </section>
        </div>
      </main>
    </div>
  );
}
