import { useEffect, useMemo, useState } from "react";
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
  CalendarBlankIcon,
  CheckCircleIcon,
  FunnelIcon,
  InfoIcon,
  WarningCircleIcon,
  XCircleIcon
} from "@phosphor-icons/react";
import type { Icon } from "@phosphor-icons/react";
import { Area, AreaChart, ResponsiveContainer } from "recharts";
import {
  Button,
  Collapsible,
  Combobox,
  DatePicker,
  Input,
  Popover,
  Select,
  Table
} from "@cloudflare/kumo";
import {
  endOfDay,
  format,
  formatDistanceToNowStrict,
  isValid,
  parseISO,
  startOfDay,
  subDays,
  subHours
} from "date-fns";

import type { AdminNode, AdminUser, NetworkEvent, NetworkEventsResponse } from "../types";
import { useAdminApi } from "@/admin/api";
import { adminKeys, queryString } from "@/admin/query";
import { AdminPagination, TableLoading } from "@/components/admin-table";
import { PageHeader, PageTopBar } from "./operations-common";

type RangePreset = "1h" | "24h" | "7d" | "30d" | "custom" | "all";

type ColumnMeta = {
  headClassName?: string;
  cellClassName?: string;
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
const chartColor = "var(--color-kumo-info)";

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

function formatEventTime(value: string): string {
  const date = parseDateParam(value);
  return date ? format(date, "MMM d, HH:mm") : "n/a";
}

function formatAgo(value: string): string {
  const date = parseDateParam(value);
  return date ? `${formatDistanceToNowStrict(date, { addSuffix: true })}` : "";
}

function actionLabel(action: string): string {
  if (action === "outbound_connect") return "Outbound";
  if (action === "invalid_connection") return "Invalid";
  if (!action) return "Unknown";
  return action.replace(/_/g, " ");
}

function actionMeta(action: string): { icon: Icon; className: string } {
  const value = action.toLowerCase();
  if (value === "connect" || value === "outbound_connect" || value === "accept") {
    return { icon: CheckCircleIcon, className: "text-kumo-success" };
  }
  if (value === "reject" || value === "block" || value === "blocked") {
    return { icon: XCircleIcon, className: "text-kumo-danger" };
  }
  if (value === "invalid_connection") {
    return { icon: WarningCircleIcon, className: "text-kumo-warning" };
  }
  return { icon: InfoIcon, className: "text-kumo-subtle" };
}

function eventDestination(event: NetworkEvent): string {
  if (!event.target_host) return "n/a";
  return event.target_port ? `${event.target_host}:${event.target_port}` : event.target_host;
}

function actionCounts(events: NetworkEvent[]) {
  const counts = new Map<string, number>();
  for (const event of events) {
    counts.set(event.action || "unknown", (counts.get(event.action || "unknown") ?? 0) + event.count);
  }
  return [...counts.entries()].sort((a, b) => b[1] - a[1]);
}

function chartData(events: NetworkEvent[]) {
  const buckets = new Map<string, number>();
  for (const event of events) {
    const date = parseDateParam(event.window_end || event.created_at);
    if (!date) continue;
    const key = format(date, "HH:mm");
    buckets.set(key, (buckets.get(key) ?? 0) + event.count);
  }
  return [...buckets.entries()].reverse().map(([time, count]) => ({ time, count }));
}

function columnClass(column: { columnDef: { meta?: unknown } }, key: keyof ColumnMeta) {
  return ((column.columnDef.meta as ColumnMeta | undefined)?.[key] ?? "") as string;
}

function EventActivity({ events }: { events: NetworkEvent[] }) {
  const counts = actionCounts(events);
  const data = chartData(events);
  if (data.length === 0) return null;
  return (
    <div className="flex flex-col gap-3 px-2 pt-2 pb-1">
      <div className="flex flex-wrap gap-x-6 gap-y-1">
        {counts.map(([action, count]) => {
          const meta = actionMeta(action);
          const Icon = meta.icon;
          return (
            <span key={action} className={`inline-flex items-center gap-1 whitespace-nowrap text-sm ${meta.className}`}>
              <Icon className="size-3.5 shrink-0" />
              <span className="capitalize">{actionLabel(action)}</span>
              <span className="font-semibold tabular-nums">{count}</span>
            </span>
          );
        })}
      </div>
      <div className="h-[108px] w-full">
        <ResponsiveContainer width="100%" height="100%" initialDimension={{ width: 320, height: 80 }}>
          <AreaChart data={data} margin={{ top: 4, right: 0, bottom: 0, left: 0 }}>
            <defs>
              <linearGradient id="network-events-gradient" x1="0" y1="0" x2="0" y2="1">
                <stop offset="5%" stopColor={chartColor} stopOpacity={0.5} />
                <stop offset="95%" stopColor={chartColor} stopOpacity={0} />
              </linearGradient>
            </defs>
            <Area
              type="monotone"
              dataKey="count"
              stroke={chartColor}
              strokeWidth={1.4}
              fill="url(#network-events-gradient)"
              fillOpacity={0.35}
              dot={false}
              isAnimationActive={false}
            />
          </AreaChart>
        </ResponsiveContainer>
      </div>
    </div>
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
    next.set("limit", String(nextLimit));
    if (nextOffset > 0) next.set("offset", String(nextOffset));
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

  const path = "/api/admin/network-events" + queryString({
    search: filters.search.trim() || undefined,
    action: filters.action === "all" ? undefined : filters.action,
    node: filters.node === "all" ? undefined : filters.node,
    user: filters.user === "all" ? undefined : filters.user,
    start: timeRange.start,
    end: timeRange.end,
    limit: perPage,
    offset
  });
  const eventsQuery = useQuery({
    // Preset ranges deliberately key on the preset instead of their exact
    // millisecond timestamps. This lets a quick route revisit use TanStack's
    // short-lived cache; once stale, the current queryFn still fetches a fresh
    // time window. Custom ranges retain their exact boundaries in the key.
    queryKey: adminKeys.networkEvents({
      search: filters.search.trim(),
      action: filters.action,
      node: filters.node,
      user: filters.user,
      range: filters.range,
      start: filters.range === "custom" ? timeRange.start : undefined,
      end: filters.range === "custom" ? timeRange.end : undefined,
      limit: perPage,
      offset,
      refreshGeneration
    }),
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
  const error = eventsQuery.error instanceof Error ? eventsQuery.error.message : "Request failed.";
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
    filters.action !== "all",
    filters.node !== "all",
    filters.user !== "all"
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
        <div className="flex min-w-0 items-baseline justify-between gap-3 whitespace-nowrap">
          <span className="text-kumo-default">{formatEventTime(info.getValue())}</span>
          <span className="text-xs text-kumo-subtle">{formatAgo(info.row.original.created_at)}</span>
        </div>
      ),
      meta: { headClassName: "w-56", cellClassName: "w-56" }
    }),
    columnHelper.accessor("action", {
      header: "Action",
      cell: (info) => {
        const meta = actionMeta(info.getValue());
        const Icon = meta.icon;
        return (
          <span className={`inline-flex items-center gap-1.5 whitespace-nowrap text-sm font-medium ${meta.className}`}>
            <Icon className="size-4 shrink-0" />
            <span className="capitalize">{actionLabel(info.getValue())}</span>
          </span>
        );
      },
      meta: { headClassName: "w-36", cellClassName: "w-36" }
    }),
    columnHelper.accessor("user_name", {
      header: "User",
      cell: (info) => <span className="block truncate text-kumo-default" title={info.getValue()}>{info.getValue() || "n/a"}</span>,
      meta: {
        headClassName: "sticky left-0 z-20 w-40 bg-kumo-base",
        cellClassName: "sticky left-0 z-10 w-40 bg-kumo-base"
      }
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
      <PageTopBar current="Network Events" />
      <main className="w-full grow bg-kumo-canvas">
        <PageHeader
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

        <div className="mx-auto flex w-full max-w-[1400px] flex-col gap-4 px-6 pb-8 md:px-8 lg:px-10">
          <section className="flex flex-col gap-3">
            <div className="flex flex-col gap-1 sm:flex-row sm:items-end sm:justify-between">
              <div>
                <h2 className="text-base font-semibold text-kumo-default">Gateway events</h2>
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
                    Filter{activeFilterCount > 0 ? ` ${activeFilterCount}` : ""}
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

            <EventActivity events={events} />

            <div className="overflow-hidden rounded-lg border border-kumo-line bg-kumo-base">
              <div className="bf-table-scroll overflow-x-auto overscroll-x-contain">
                <Table className="min-w-[1600px] table-fixed">
                  <Table.Header variant="compact">
                    {table.getHeaderGroups().map((headerGroup) => (
                      <Table.Row key={headerGroup.id}>
                        {headerGroup.headers.map((header) => (
                          <Table.Head key={header.id} className={columnClass(header.column, "headClassName")}>
                            {header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}
                          </Table.Head>
                        ))}
                      </Table.Row>
                    ))}
                  </Table.Header>
                  <Table.Body>
                    {eventsQuery.error ? (
                      <Table.Row>
                        <Table.Cell colSpan={columns.length}>
                          <div className="flex min-h-32 items-center justify-center text-sm text-kumo-subtle">{error}</div>
                        </Table.Cell>
                      </Table.Row>
                    ) : eventsQuery.isLoading ? (
                      <TableLoading colSpan={columns.length} />
                    ) : table.getRowModel().rows.length > 0 ? (
                      table.getRowModel().rows.map((row) => (
                        <Table.Row key={row.id}>
                          {row.getVisibleCells().map((cell) => (
                            <Table.Cell key={cell.id} className={columnClass(cell.column, "cellClassName")}>
                              {flexRender(cell.column.columnDef.cell, cell.getContext())}
                            </Table.Cell>
                          ))}
                        </Table.Row>
                      ))
                    ) : (
                      <Table.Row>
                        <Table.Cell colSpan={columns.length}>
                          <div className="flex min-h-32 items-center justify-center text-sm text-kumo-subtle">No events match this filter.</div>
                        </Table.Cell>
                      </Table.Row>
                    )}
                  </Table.Body>
                </Table>
              </div>
            </div>

            <AdminPagination page={page} setPage={setPage} perPage={perPage} setPerPage={setPageSize} total={total} />
          </section>
        </div>
      </main>
    </div>
  );
}
