import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowClockwiseIcon, FunnelIcon, TerminalWindowIcon } from "@phosphor-icons/react";
import { Badge, Banner, Button, Collapsible, Combobox, Dialog, Input, Select, Table } from "@cloudflare/kumo";

import type { SystemLog, SystemLogsResponse } from "../types";
import { useAdminApi } from "@/admin/api";
import { adminKeys, queryString } from "@/admin/query";
import { AdminPagination, SortHead, TableCard, TableEmpty, TableError, TableLoading } from "@/components/admin-table";
import { AppPageHeader } from "@/components/app-page-header";
import { StatusBadge, type StatusTone } from "@/components/status-badge";

type LevelFilter = "all" | "error" | "warn" | "info" | "debug";
type LogSort = "observed_at" | "node" | "service" | "level" | "message" | "ingested_at";
type SortDirection = "asc" | "desc";

const fetchLimitOptions = [100, 250, 500] as const;

function normalizeLevel(level: string): Exclude<LevelFilter, "all"> {
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
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit"
  }).format(date);
}

function timeValue(value: string): number {
  const time = new Date(value).getTime();
  return Number.isFinite(time) ? time : 0;
}

function compareText(left: string | number | undefined, right: string | number | undefined, direction: SortDirection) {
  return String(left ?? "").localeCompare(String(right ?? ""), undefined, { numeric: true }) * (direction === "desc" ? -1 : 1);
}

function sortLogs(logs: SystemLog[], sort: LogSort, direction: SortDirection): SystemLog[] {
  const factor = direction === "desc" ? -1 : 1;
  return [...logs].sort((a, b) => {
    switch (sort) {
      case "node":
        return compareText(a.node, b.node, direction) || compareText(a.service, b.service, "asc");
      case "service":
        return compareText(a.service, b.service, direction) || compareText(a.node, b.node, "asc");
      case "level":
        return compareText(normalizeLevel(a.level), normalizeLevel(b.level), direction) || compareText(a.node, b.node, "asc");
      case "message":
        return compareText(a.message, b.message, direction) || compareText(a.node, b.node, "asc");
      case "ingested_at":
        return (timeValue(a.ingested_at) - timeValue(b.ingested_at)) * factor || compareText(a.node, b.node, "asc");
      default:
        return (timeValue(a.observed_at) - timeValue(b.observed_at)) * factor || compareText(a.node, b.node, "asc");
    }
  });
}

export function SystemLogsPage() {
  const { request } = useAdminApi();
  const [page, setPage] = useState(1);
  const [perPage, setPerPage] = useState(25);
  const [fetchLimit, setFetchLimit] = useState<(typeof fetchLimitOptions)[number]>(100);
  const [filterOpen, setFilterOpen] = useState(false);
  const [level, setLevel] = useState<LevelFilter>("all");
  const [node, setNode] = useState("all");
  const [service, setService] = useState("all");
  const [searchInput, setSearchInput] = useState("");
  const [search, setSearch] = useState("");
  const [sort, setSortValue] = useState<LogSort>("observed_at");
  const [direction, setDirection] = useState<SortDirection>("desc");
  const [detail, setDetail] = useState<SystemLog | null>(null);

  const path = "/api/admin/system-logs" + queryString({ limit: fetchLimit });
  const logsQuery = useQuery({
    queryKey: adminKeys.systemLogs(fetchLimit),
    queryFn: () => request<SystemLogsResponse>(path),
    placeholderData: (previous) => previous
  });

  const logs = useMemo(() => logsQuery.data?.logs ?? [], [logsQuery.data?.logs]);
  const note = logsQuery.data?.note ?? "";
  const nodeOptions = useMemo(() => Array.from(new Set(logs.map((log) => log.node).filter(Boolean))).sort(), [logs]);
  const serviceOptions = useMemo(() => Array.from(new Set(logs.map((log) => log.service).filter(Boolean))).sort(), [logs]);
  const nodeChoices = useMemo(() => ["all", ...nodeOptions], [nodeOptions]);
  const serviceChoices = useMemo(() => ["all", ...serviceOptions], [serviceOptions]);
  const activeFilterCount = [level !== "all", node !== "all", service !== "all"].filter(Boolean).length;

  function setSort(column: LogSort) {
    setPage(1);
    if (sort === column) {
      setDirection((value) => (value === "asc" ? "desc" : "asc"));
      return;
    }
    setSortValue(column);
    setDirection(column === "observed_at" || column === "ingested_at" ? "desc" : "asc");
  }

  function setPageSize(value: number) {
    setPerPage(value);
    setPage(1);
  }

  function setLevelFilter(value: LevelFilter) {
    setLevel(value);
    setPage(1);
  }

  function setNodeFilter(value: string) {
    setNode(value);
    setPage(1);
  }

  function setServiceFilter(value: string) {
    setService(value);
    setPage(1);
  }

  function setLimit(value: string) {
    const parsed = Number(value);
    if (fetchLimitOptions.includes(parsed as (typeof fetchLimitOptions)[number])) {
      setFetchLimit(parsed as (typeof fetchLimitOptions)[number]);
      setPage(1);
    }
  }

  function resetFilters() {
    setLevel("all");
    setNode("all");
    setService("all");
    setFetchLimit(100);
    setPage(1);
  }

  const filtered = useMemo(() => {
    const needle = search.trim().toLowerCase();
    return sortLogs(
      logs.filter((log) => {
        if (level !== "all" && normalizeLevel(log.level) !== level) return false;
        if (node !== "all" && log.node !== node) return false;
        if (service !== "all" && log.service !== service) return false;
        if (!needle) return true;
        return [log.node, log.service, log.level, log.message].some((value) => value.toLowerCase().includes(needle));
      }),
      sort,
      direction
    );
  }, [direction, level, logs, node, search, service, sort]);

  const offset = (page - 1) * perPage;
  const visibleRows = filtered.slice(offset, offset + perPage);
  const total = filtered.length;
  const isRefreshing = logsQuery.isFetching && !logsQuery.isLoading;

  return (
    <div className="flex min-h-full flex-col bg-kumo-canvas">
      <AppPageHeader
        title="System Logs"
        description="Inspect recent agent, sing-box, and service journal entries reported by nodes."
        actions={
          <Button
            variant="secondary"
            icon={ArrowClockwiseIcon}
            disabled={logsQuery.isFetching}
            onClick={() => void logsQuery.refetch()}
          >
            Refresh
          </Button>
        }
      />
      <main className="w-full grow bg-kumo-canvas">
        <div className="mx-auto flex w-full max-w-[1400px] flex-col gap-4 px-6 pb-8 md:px-8 lg:px-10">
          <section className="flex flex-col gap-3">
            <div>
              <h2 className="text-base font-semibold text-kumo-default">Recent logs</h2>
              <p className="text-sm text-kumo-subtle">
                {total > 0 ? `Showing ${offset + 1}-${Math.min(offset + perPage, total)} of ${total}` : "No logs"}
                {logs.length > 0 ? `, ${logs.length} fetched` : ""}
              </p>
            </div>

            <Collapsible.Root open={filterOpen} onOpenChange={setFilterOpen}>
              <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
                <form
                  className="flex min-w-0 flex-1 gap-2"
                  onSubmit={(event) => {
                    event.preventDefault();
                    setSearch(searchInput.trim());
                    setPage(1);
                  }}
                >
                  <Input
                    placeholder="Search by node, service, level, or message"
                    aria-label="Search system logs"
                    value={searchInput}
                    onChange={(event) => setSearchInput(event.target.value)}
                    className="min-w-0 flex-1"
                  />
                  <Button type="submit" variant="secondary">
                    Search
                  </Button>
                </form>
                <Collapsible.Trigger render={<Button type="button" variant="secondary" icon={FunnelIcon} />}>
                  Filter
                  {activeFilterCount > 0 ? (
                    <Badge variant="secondary" className="ml-1.5">
                      {activeFilterCount}
                    </Badge>
                  ) : null}
                </Collapsible.Trigger>
              </div>

              <Collapsible.Panel className="rounded-lg bg-kumo-tint p-3">
                <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
                  <Select
                    label="Level"
                    value={level}
                    onValueChange={(value) => setLevelFilter((value ?? "all") as LevelFilter)}
                    items={{
                      all: "All levels",
                      error: "Error",
                      warn: "Warning",
                      info: "Info",
                      debug: "Debug"
                    }}
                  />

                  <Combobox
                    label="Service"
                    value={service}
                    onValueChange={(value) => setServiceFilter((value as string | null) ?? "all")}
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
                    value={node}
                    onValueChange={(value) => setNodeFilter((value as string | null) ?? "all")}
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

                  <Select
                    label="Fetch limit"
                    value={String(fetchLimit)}
                    onValueChange={(value) => setLimit(value ?? "100")}
                    items={{ "100": "100", "250": "250", "500": "500" }}
                  />
                </div>
                <div className="mt-3 flex justify-end gap-2">
                  <Button variant="secondary" size="sm" onClick={resetFilters}>
                    Reset
                  </Button>
                  <Button variant="secondary" size="sm" onClick={() => setFilterOpen(false)}>
                    Done
                  </Button>
                </div>
              </Collapsible.Panel>
            </Collapsible.Root>

            {note ? <Banner variant="secondary" title={note} /> : null}

            <TableCard>
              <Table className={`min-w-[900px] table-fixed transition-opacity ${isRefreshing ? "opacity-60" : ""}`}>
                <Table.Header variant="compact">
                  <Table.Row>
                    <SortHead label="Observed" column="observed_at" sort={sort} direction={direction} setSort={setSort} sticky="left" className="w-40" />
                    <SortHead label="Node" column="node" sort={sort} direction={direction} setSort={setSort} className="w-40" />
                    <SortHead label="Service" column="service" sort={sort} direction={direction} setSort={setSort} className="w-36" />
                    <SortHead label="Level" column="level" sort={sort} direction={direction} setSort={setSort} className="w-28" />
                    <SortHead label="Message" column="message" sort={sort} direction={direction} setSort={setSort} className="w-[35%]" />
                    <SortHead label="Ingested" column="ingested_at" sort={sort} direction={direction} setSort={setSort} className="w-40" />
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {logsQuery.error ? (
                    <TableError colSpan={6}>
                      {logsQuery.error instanceof Error ? logsQuery.error.message : "Request failed."}
                    </TableError>
                  ) : logsQuery.isLoading ? (
                    <TableLoading colSpan={6} />
                  ) : visibleRows.length > 0 ? (
                    visibleRows.map((log) => {
                      const meta = levelBadge(log.level);
                      const rowKey = `${log.observed_at}|${log.ingested_at}|${log.node}|${log.service}|${log.message.slice(0, 24)}`;
                      return (
                        <Table.Row key={rowKey}>
                          <Table.Cell sticky="left" className="w-40">
                            <span className="whitespace-nowrap text-kumo-subtle" title={log.observed_at || undefined}>
                              {formatTimestamp(log.observed_at)}
                            </span>
                          </Table.Cell>
                          <Table.Cell className="w-40">
                            <span className="block truncate text-kumo-default" title={log.node || undefined}>
                              {log.node || "—"}
                            </span>
                          </Table.Cell>
                          <Table.Cell className="w-36">
                            <span className="flex min-w-0 items-center gap-1.5 whitespace-nowrap text-kumo-subtle">
                              <TerminalWindowIcon className="size-4 shrink-0" />
                              <span className="truncate">{log.service || "—"}</span>
                            </span>
                          </Table.Cell>
                          <Table.Cell className="w-28">
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
                          <Table.Cell className="w-40">
                            <span className="whitespace-nowrap text-kumo-subtle" title={log.ingested_at || undefined}>
                              {formatTimestamp(log.ingested_at)}
                            </span>
                          </Table.Cell>
                        </Table.Row>
                      );
                    })
                  ) : (
                    <TableEmpty colSpan={6}>No logs match this filter.</TableEmpty>
                  )}
                </Table.Body>
              </Table>
            </TableCard>

            <AdminPagination page={page} setPage={setPage} perPage={perPage} setPerPage={setPageSize} total={total} />
          </section>
        </div>
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
