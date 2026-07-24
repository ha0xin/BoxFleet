import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import type { Icon } from "@phosphor-icons/react";
import {
  ArrowClockwiseIcon,
  CheckCircleIcon,
  FunnelIcon,
  InfoIcon,
  TerminalWindowIcon,
  WarningCircleIcon,
  XCircleIcon
} from "@phosphor-icons/react";
import { Button, Collapsible, Combobox, Input, Select, Table } from "@cloudflare/kumo";

import type { SystemLog, SystemLogsResponse } from "../types";
import { useAdminApi } from "@/admin/api";
import { adminKeys, queryString } from "@/admin/query";
import { AdminPagination, SortHead, TableEmpty, TableLoading } from "@/components/admin-table";
import { PageHeader, PageTopBar } from "./operations-common";

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

function levelMeta(level: string): { label: string; icon: Icon; className: string } {
  switch (normalizeLevel(level)) {
    case "error":
      return { label: level || "error", icon: XCircleIcon, className: "text-kumo-danger" };
    case "warn":
      return { label: level || "warn", icon: WarningCircleIcon, className: "text-kumo-warning" };
    case "debug":
      return { label: level || "debug", icon: InfoIcon, className: "text-kumo-subtle" };
    default:
      return { label: level || "info", icon: CheckCircleIcon, className: "text-kumo-info" };
  }
}

function formatTimestamp(value: string): string {
  if (!value) return "n/a";
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return value;
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
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
  const activeFilterCount = [
    level !== "all",
    node !== "all",
    service !== "all",
    fetchLimit !== 100
  ].filter(Boolean).length;

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
  const error = logsQuery.error instanceof Error ? logsQuery.error.message : "Request failed.";

  return (
    <div className="flex min-h-full flex-col bg-kumo-canvas">
      <PageTopBar current="System Logs" />
      <main className="w-full grow bg-kumo-canvas">
        <PageHeader
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

        <div className="mx-auto flex w-full max-w-[1400px] flex-col gap-4 px-6 pb-8 md:px-8 lg:px-10">
          <section className="flex flex-col gap-3">
            <div className="flex flex-col gap-1 sm:flex-row sm:items-end sm:justify-between">
              <div>
                <h2 className="text-base font-semibold text-kumo-default">Recent logs</h2>
                <p className="text-sm text-kumo-subtle">
                  {total > 0 ? `Showing ${offset + 1}-${Math.min(offset + perPage, total)} of ${total}` : "No logs"}
                  {logs.length > 0 ? `, ${logs.length} fetched` : ""}
                </p>
              </div>
              {note ? <p className="max-w-xl text-sm text-kumo-subtle">{note}</p> : null}
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
                  Filter{activeFilterCount > 0 ? ` ${activeFilterCount}` : ""}
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
                  <Button variant="primary" size="sm" onClick={() => setFilterOpen(false)}>
                    Done
                  </Button>
                </div>
              </Collapsible.Panel>
            </Collapsible.Root>

            <div className="overflow-hidden rounded-lg border border-kumo-line bg-kumo-base">
              <div className="bf-table-scroll overflow-x-auto overscroll-x-contain">
                <Table className="min-w-[900px] table-fixed">
                  <Table.Header variant="compact">
                    <Table.Row>
                      <SortHead label="Observed" column="observed_at" sort={sort} direction={direction} setSort={setSort} className="w-36" />
                      <SortHead label="Node" column="node" sort={sort} direction={direction} setSort={setSort} className="sticky left-0 z-20 w-40 bg-kumo-base" />
                      <SortHead label="Service" column="service" sort={sort} direction={direction} setSort={setSort} className="w-36" />
                      <SortHead label="Level" column="level" sort={sort} direction={direction} setSort={setSort} className="w-20" />
                      <SortHead label="Message" column="message" sort={sort} direction={direction} setSort={setSort} className="w-[35%]" />
                      <SortHead label="Ingested" column="ingested_at" sort={sort} direction={direction} setSort={setSort} className="w-36" />
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {logsQuery.error ? (
                      <TableEmpty colSpan={6}>{error}</TableEmpty>
                    ) : logsQuery.isLoading ? (
                      <TableLoading colSpan={6} />
                    ) : visibleRows.length > 0 ? (
                      visibleRows.map((log, index) => {
                        const meta = levelMeta(log.level);
                        const LevelIcon = meta.icon;
                        const rowKey = `${log.observed_at}-${log.node}-${log.service}-${index}`;
                        return (
                          <Table.Row key={rowKey}>
                            <Table.Cell className="w-36">
                              <span className="whitespace-nowrap text-kumo-subtle">{formatTimestamp(log.observed_at)}</span>
                            </Table.Cell>
                            <Table.Cell className="sticky left-0 z-10 w-40 bg-kumo-base">
                              <span className="block truncate text-kumo-default" title={log.node}>{log.node || "n/a"}</span>
                            </Table.Cell>
                            <Table.Cell className="w-36">
                              <span className="flex min-w-0 items-center gap-1.5 whitespace-nowrap text-kumo-subtle">
                                <TerminalWindowIcon className="size-4 shrink-0" />
                                <span className="truncate">{log.service || "system"}</span>
                              </span>
                            </Table.Cell>
                            <Table.Cell className="w-20">
                              <span className={`inline-flex items-center gap-1.5 whitespace-nowrap text-sm font-medium ${meta.className}`}>
                                <LevelIcon className="size-4 shrink-0" />
                                {meta.label}
                              </span>
                            </Table.Cell>
                            <Table.Cell>
                              <span className="block truncate text-kumo-default" title={log.message}>
                                {log.message || "n/a"}
                              </span>
                            </Table.Cell>
                            <Table.Cell className="w-36">
                              <span className="whitespace-nowrap text-kumo-subtle">{formatTimestamp(log.ingested_at)}</span>
                            </Table.Cell>
                          </Table.Row>
                        );
                      })
                    ) : (
                      <TableEmpty colSpan={6}>No logs match this filter.</TableEmpty>
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
