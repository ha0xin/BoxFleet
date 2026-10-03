import { Fragment, useEffect, useMemo, useState } from "react";
import { format } from "date-fns";
import { zodResolver } from "@hookform/resolvers/zod";
import { useQuery } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { ArrowClockwiseIcon, FunnelIcon } from "@phosphor-icons/react";
import { Badge, Banner, Button, Collapsible, Combobox, Dialog, Input, Select, Table } from "@cloudflare/kumo";

import type { AdminNode, SystemLog, SystemLogLevelFilter, SystemLogSort, SystemLogsResponse } from "../types";
import { useAdminApi } from "@/admin/api";
import { adminKeys, queryString, refreshIntervals } from "@/admin/query";
import { useUrlFilters, type UseUrlFiltersOptions } from "@/admin/use-url-filters";
import {
  AdminPagination,
  SortHead,
  TableCard,
  TableColgroup,
  TableEmpty,
  TableError,
  TableLoading,
  tableMinWidth
} from "@/components/admin-table";
import type { TableColumnWidth } from "@/components/admin-table";
import { LogExpandButton, LogRowDetails } from "@/components/log-row-details";
import { LogWorkspace, LogFieldsToggle } from "@/components/log-workspace";
import { AppPageHeader } from "@/components/app-page-header";
import { StatusBadge, type StatusTone } from "@/components/status-badge";

/**
 * Filtering, sorting and paging all happen in `/api/admin/system-logs`.
 *
 * The page used to pull a flat "last N entries" list and slice it in the
 * browser, which made every control a lie about the archive: a level filter
 * could only reach the fetched window, and raising the window to compensate
 * shipped rows nobody looked at. The URL now carries the whole query, so a
 * filtered view is linkable and survives a refresh.
 */

// `satisfies` pins these lists to the API contract in types.ts: a server-side
// rename becomes a build error here instead of a silently ignored parameter.
const sortColumns = ["observed_at", "node", "service", "level", "message", "ingested_at"] as const satisfies
  readonly SystemLogSort[];
const levelFilters = ["error", "warn", "info", "debug"] as const satisfies readonly SystemLogLevelFilter[];

type LogSort = (typeof sortColumns)[number];

// "all" is the page's sentinel for "no filter"; it is never sent to the server,
// which has no such value (a node may legitimately be named "all").
const filterSchema = z.object({
  search: z.string(),
  level: z.enum(["all", ...levelFilters]),
  node: z.string(),
  service: z.string(),
  sort: z.enum(sortColumns),
  direction: z.enum(["asc", "desc"]),
  range: z.enum(["all", "1h", "24h", "7d"])
});

type FilterValues = z.infer<typeof filterSchema>;

// Newest first, matching how a journal is read and the server's own default.
const defaultFilters: FilterValues = {
  search: "",
  level: "all",
  node: "all",
  service: "all",
  sort: "observed_at",
  direction: "desc",
  range: "all"
};

const urlFilterOptions: UseUrlFiltersOptions<FilterValues> = {
  schema: filterSchema,
  defaults: defaultFilters,
  perPage: 25
};

const levelItems = {
  all: "All levels",
  error: "Error",
  warn: "Warning",
  info: "Info",
  debug: "Debug"
} as const;

/**
 * Column widths, in table order. Timestamps, level badges, node and service
 * names all have a hard ceiling; the message is the only column that can use
 * more room, so it is the single flexible one and every surplus pixel lands
 * there instead of being smeared over the other five.
 */
const logColumns: TableColumnWidth[] = [
  36, // Details
  270, // Observed
  152, // Node
  140, // Service
  112, // Level
  { min: 360 }, // Message
  152 // Ingested
];

const COLUMN_COUNT = logColumns.length;

/** Journald levels are free text, so the badge buckets them the way the server does. */
function normalizeLevel(level: string): Exclude<FilterValues["level"], "all"> {
  const value = (level || "").trim().toLowerCase();
  if (value.includes("fatal") || value.includes("error")) return "error";
  if (value.includes("warn")) return "warn";
  if (value.includes("debug") || value.includes("trace")) return "debug";
  return "info";
}

function levelBadge(level: string): { label: string; tone: StatusTone } {
  switch (normalizeLevel(level)) {
    case "error":
      return { label: "Error", tone: "error" };
    case "warn":
      return { label: "Warning", tone: "warning" };
    case "debug":
      return { label: "Debug", tone: "neutral" };
    default:
      return { label: "Info", tone: "info" };
  }
}

function formatTimestamp(value: string): string {
  if (!value) return "—";
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return value;
  return format(date, "yyyy-MM-dd HH:mm:ss.SSS");
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "Request failed.";
}

/**
 * "all" plus the server's option list, with the active value forced in.
 *
 * Options come from the server rather than from the visible rows because one
 * page cannot enumerate them and because a filter's choices must not move when
 * the filter is applied. A link can still name a node or service that has since
 * aged out, and the control has to keep showing what it is filtering by.
 */
export function choiceList(options: readonly string[] | undefined, active: string): string[] {
  const values = new Set((options ?? []).filter(Boolean));
  if (active !== "all") values.add(active);
  return ["all", ...[...values].sort()];
}

export function SystemLogsPage() {
  const { request } = useAdminApi();
  const { filters, page, perPage, offset, setFilters, setPage, setPerPage, resetFilters } =
    useUrlFilters(urlFilterOptions);
  const [filterOpen, setFilterOpen] = useState(false);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [timeAnchor, setTimeAnchor] = useState(() => Date.now());
  const rangeHours = filters.range === "1h" ? 1 : filters.range === "24h" ? 24 : filters.range === "7d" ? 168 : 0;
  const timeStart = rangeHours ? new Date(timeAnchor - rangeHours * 3600000).toISOString() : undefined;
  const timeEnd = rangeHours ? new Date(timeAnchor).toISOString() : undefined;
  const [detail, setDetail] = useState<SystemLog | null>(null);

  // react-hook-form is the draft layer for the search box only; the facets
  // commit on change. `values: filters` re-syncs the draft on Back/Forward.
  const form = useForm<FilterValues>({ resolver: zodResolver(filterSchema), values: filters });

  const path = "/api/admin/system-logs" + queryString({
    limit: perPage,
    offset,
    search: filters.search,
    level: filters.level === "all" ? undefined : filters.level,
    node: filters.node === "all" ? undefined : filters.node,
    service: filters.service === "all" ? undefined : filters.service,
    sort: filters.sort,
    direction: filters.direction,
    start: timeStart,
    end: timeEnd
  });
  const logsQuery = useQuery({
    queryKey: adminKeys.systemLogs(
      perPage,
      offset,
      filters.search,
      filters.level,
      filters.node,
      filters.service,
      filters.sort,
      filters.direction,
      timeStart,
      timeEnd
    ),
    queryFn: ({ signal }) => request<SystemLogsResponse>(path, { signal }),
    placeholderData: (previous) => previous,
    refetchInterval: refreshIntervals.live
  });
  // Node options are the full node list, not the names on this page, for the
  // same reason the server sends the full service list.
  const nodesQuery = useQuery({
    queryKey: adminKeys.nodes,
    queryFn: ({ signal }) => request<AdminNode[]>("/api/admin/nodes", { signal })
  });

  const logs = useMemo(() => logsQuery.data?.logs ?? [], [logsQuery.data?.logs]);
  const note = logsQuery.data?.note ?? "";
  const total = logsQuery.data?.total ?? 0;
  const serviceChoices = useMemo(
    () => choiceList(logsQuery.data?.services, filters.service),
    [filters.service, logsQuery.data?.services]
  );
  const nodeChoices = useMemo(
    () => choiceList(nodesQuery.data?.map((node) => node.name), filters.node),
    [filters.node, nodesQuery.data]
  );

  // The hook's own `activeFilterCount` includes sort and direction, which are a
  // view preference rather than a filter; the badge counts only the facets.
  const activeFilterCount = [filters.level !== "all", filters.node !== "all", filters.service !== "all"]
    .filter(Boolean).length;
  const narrowed = filters.search !== "" || activeFilterCount > 0;

  // The hook never sees `total`, so the upper clamp lives here, in an effect —
  // calling setSearchParams during render is a navigation. It waits for a
  // response: `total` is 0 during the first fetch, and clamping then would
  // rewrite a shared `?page=3` before its rows ever arrive.
  const lastPage = Math.max(1, Math.ceil(total / perPage));
  useEffect(() => {
    if (logsQuery.data && page > lastPage) setPage(lastPage, "replace");
  }, [lastPage, logsQuery.data, page, setPage]);

  function setSort(column: LogSort) {
    setFilters((current) =>
      current.sort === column
        ? { direction: current.direction === "asc" ? "desc" : "asc" }
        // Time columns read newest-first; text columns read A-Z.
        : { sort: column, direction: column === "observed_at" || column === "ingested_at" ? "desc" : "asc" }
    );
  }

  const isRefreshing = logsQuery.isFetching && !logsQuery.isLoading;

  return (
    // `min-w-0`: this div is a grid item, and without it the table's min-width
    // becomes the page's min-width and the whole page scrolls sideways instead
    // of the table.
    <div className="flex min-h-full min-w-0 flex-col bg-kumo-canvas">
      <AppPageHeader
        compact
        title="System Logs"
        description="Inspect recent agent, sing-box, and service journal entries reported by nodes."
        actions={
          <Button
            variant="secondary"
            icon={ArrowClockwiseIcon}
            disabled={logsQuery.isFetching}
            onClick={() => { setTimeAnchor(Date.now()); void logsQuery.refetch(); }}
          >
            Refresh
          </Button>
        }
      />
      <main className="w-full grow bg-kumo-canvas">
        <LogWorkspace>
        <div className="flex w-full min-w-0 flex-col gap-4">
          <section className="flex flex-col gap-3">

            <Collapsible.Root open={filterOpen} onOpenChange={setFilterOpen}>
              <div className="bf-log-toolbar flex flex-wrap items-center gap-2">
                <LogFieldsToggle />
                <Collapsible.Trigger render={<Button type="button" variant="secondary" icon={FunnelIcon} />}>Add filter{activeFilterCount > 0 ? <Badge variant="secondary" className="ml-1.5">{activeFilterCount}</Badge> : null}</Collapsible.Trigger>
                <form
                  className="flex min-w-0 flex-1 gap-2"
                  onSubmit={form.handleSubmit((values) => setFilters({ search: values.search.trim() }))}
                >
                  <Input
                    placeholder="Search logs…"
                    aria-label="Search system logs"
                    className="min-w-0 flex-1"
                    {...form.register("search")}
                  />
                  <Button type="submit" variant="secondary">
                    Search
                  </Button>
                </form>
                <Select aria-label="Time range" value={filters.range} onValueChange={(value) => { setTimeAnchor(Date.now()); setFilters({ range: (value ?? "all") as FilterValues["range"] }); }} items={[{value:"all", label:"All time"}, {value:"1h", label:"Last 1 hour"}, {value:"24h", label:"Last 24 hours"}, {value:"7d", label:"Last 7 days"}]} />

              </div>

              <Collapsible.Panel className="rounded-lg bg-kumo-tint p-3">
                <div className="grid gap-3 md:grid-cols-3">
                  <Select
                    label="Level"
                    value={filters.level}
                    onValueChange={(value) => setFilters({ level: (value ?? "all") as FilterValues["level"] })}
                    items={levelItems}
                  />

                  <Combobox
                    label="Service"
                    value={filters.service}
                    onValueChange={(value) => setFilters({ service: (value as string | null) ?? "all" })}
                    items={serviceChoices}
                  >
                    <Combobox.TriggerValue placeholder="All services">
                      {(value) => (value === "all" ? "All services" : value)}
                    </Combobox.TriggerValue>
                    <Combobox.Content>
                      <Combobox.Input placeholder="Search services" />
                      <Combobox.Empty />
                      <Combobox.List>
                        {(item: string) => (
                          <Combobox.Item key={item} value={item}>
                            {item === "all" ? "All services" : item}
                          </Combobox.Item>
                        )}
                      </Combobox.List>
                    </Combobox.Content>
                  </Combobox>

                  <Combobox
                    label="Node"
                    value={filters.node}
                    onValueChange={(value) => setFilters({ node: (value as string | null) ?? "all" })}
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
                </div>
                <div className="mt-3 flex justify-end gap-2">
                  <Button variant="secondary" size="sm" onClick={() => resetFilters()}>
                    Reset
                  </Button>
                  <Button variant="secondary" size="sm" onClick={() => setFilterOpen(false)}>
                    Done
                  </Button>
                </div>
              </Collapsible.Panel>
            </Collapsible.Root>

            {note ? <Banner variant="secondary" title={note} /> : null}

            <TableCard tableId="system-logs" variant="log">
              <Table
                layout="fixed"
                style={{ minWidth: tableMinWidth(logColumns) }}
                className={`transition-opacity ${isRefreshing ? "opacity-60" : ""}`}
              >
                <TableColgroup widths={logColumns} />
                <Table.Header variant="compact">
                  <Table.Row>
                    <Table.Head><span className="sr-only">Details</span></Table.Head>
                    <SortHead label="Timestamp" column="observed_at" sort={filters.sort} direction={filters.direction} setSort={setSort} />
                    <SortHead label="Node" column="node" sort={filters.sort} direction={filters.direction} setSort={setSort} />
                    <SortHead label="Service" column="service" sort={filters.sort} direction={filters.direction} setSort={setSort} />
                    <SortHead label="Level" column="level" sort={filters.sort} direction={filters.direction} setSort={setSort} />
                    <SortHead label="Message" column="message" sort={filters.sort} direction={filters.direction} setSort={setSort} />
                    <SortHead label="Ingested" column="ingested_at" sort={filters.sort} direction={filters.direction} setSort={setSort} />
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {logsQuery.error ? (
                    <TableError colSpan={COLUMN_COUNT}>{errorMessage(logsQuery.error)}</TableError>
                  ) : logsQuery.isLoading ? (
                    <TableLoading colSpan={COLUMN_COUNT} />
                  ) : logs.length > 0 ? (
                    logs.map((log) => {
                      const meta = levelBadge(log.level);
                      const rowKey = `${log.observed_at}|${log.ingested_at}|${log.node}|${log.service}|${log.message.slice(0, 24)}`;
                      return (
                        <Fragment key={rowKey}><Table.Row>
                          <Table.Cell className="bf-log-expand"><LogExpandButton expanded={expanded === rowKey} onClick={() => setExpanded(expanded === rowKey ? null : rowKey)} label="log entry" /></Table.Cell>
                          <Table.Cell>
                            <span className="block truncate font-mono text-xs text-kumo-default" title={log.observed_at || undefined}>
                              {formatTimestamp(log.observed_at)}
                            </span>
                          </Table.Cell>
                          <Table.Cell>
                            <span className="block truncate text-kumo-default" title={log.node || undefined}>
                              {log.node || "—"}
                            </span>
                          </Table.Cell>
                          <Table.Cell>
                            <span className="flex min-w-0 items-center gap-1.5 text-kumo-subtle">
                              <span className="truncate">{log.service || "—"}</span>
                            </span>
                          </Table.Cell>
                          <Table.Cell>
                            <StatusBadge tone={meta.tone}>{meta.label}</StatusBadge>
                          </Table.Cell>
                          <Table.Cell>
                            <button
                              type="button"
                              className="block w-full min-w-0 truncate text-left text-kumo-default hover:underline"
                              title={log.message || undefined}
                              aria-label={`View full log message from ${log.node || "unknown node"}`}
                              onClick={() => setDetail(log)}
                            >
                              {log.message || "—"}
                            </button>
                          </Table.Cell>
                          <Table.Cell>
                            <span className="block truncate text-kumo-subtle" title={log.ingested_at || undefined}>
                              {formatTimestamp(log.ingested_at)}
                            </span>
                          </Table.Cell>
                        </Table.Row>
                        {expanded === rowKey ? <LogRowDetails value={log} colSpan={COLUMN_COUNT} /> : null}
                        </Fragment>
                      );
                    })
                  ) : narrowed ? (
                    <TableEmpty colSpan={COLUMN_COUNT} description="Widen the search or clear the filters to see more entries.">
                      No logs match this filter
                    </TableEmpty>
                  ) : (
                    // An unfiltered empty archive is not a dead end: nodes only
                    // report journal entries once their agent is running, so say
                    // so rather than implying something was filtered away.
                    <TableEmpty
                      colSpan={COLUMN_COUNT}
                      description="Node agents upload journal entries as they run. Entries appear here once a node reports them."
                    >
                      No logs yet
                    </TableEmpty>
                  )}
                </Table.Body>
              </Table>
            </TableCard>

            <AdminPagination page={page} setPage={setPage} perPage={perPage} setPerPage={setPerPage} total={total} />
          </section>
        </div>
      </LogWorkspace>
      </main>
      {detail ? <LogDetailDialog log={detail} onClose={() => setDetail(null)} /> : null}
    </div>
  );
}

function LogDetailDialog({ log, onClose }: { log: SystemLog; onClose: () => void }) {
  const meta = levelBadge(log.level);
  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog size="lg" className="max-h-[calc(100dvh-2rem)] overflow-y-auto p-6">
        <Dialog.Title className="text-xl font-semibold text-kumo-default">Log entry</Dialog.Title>
        <Dialog.Description className="mb-4 text-kumo-subtle">
          Full journal entry as reported by the node.
        </Dialog.Description>
        <div className="mb-4 grid gap-3 sm:grid-cols-2">
          <div className="flex flex-col gap-0.5">
            <span className="text-xs font-medium text-kumo-subtle">Node</span>
            <span className="text-sm text-kumo-default">{log.node || "—"}</span>
          </div>
          <div className="flex flex-col gap-0.5">
            <span className="text-xs font-medium text-kumo-subtle">Service</span>
            <span className="text-sm text-kumo-default">{log.service || "—"}</span>
          </div>
          <div className="flex flex-col gap-0.5">
            <span className="text-xs font-medium text-kumo-subtle">Level</span>
            <span>
              <StatusBadge tone={meta.tone}>{meta.label}</StatusBadge>
            </span>
          </div>
          <div className="flex flex-col gap-0.5">
            <span className="text-xs font-medium text-kumo-subtle">Observed</span>
            <span className="text-sm text-kumo-default" title={log.observed_at || undefined}>
              {formatTimestamp(log.observed_at)}
            </span>
          </div>
          <div className="flex flex-col gap-0.5">
            <span className="text-xs font-medium text-kumo-subtle">Ingested</span>
            <span className="text-sm text-kumo-default" title={log.ingested_at || undefined}>
              {formatTimestamp(log.ingested_at)}
            </span>
          </div>
        </div>
        <pre className="max-h-80 overflow-y-auto whitespace-pre-wrap break-words rounded-lg bg-kumo-tint p-3 font-mono text-sm text-kumo-default">
          {log.message || "—"}
        </pre>
        <div className="mt-2 flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose}>
            Close
          </Button>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
