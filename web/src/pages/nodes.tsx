import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  DotsThreeIcon,
  FunnelIcon,
  HardDrivesIcon,
  PlusIcon,
  SortAscendingIcon,
  SortDescendingIcon
} from "@phosphor-icons/react";
import { Button, DropdownMenu, Input, Link, LinkButton, Loader, Pagination, Table } from "@cloudflare/kumo";

import type { AdminNode, AdminNodesResponse } from "../types";
import {
  adminPath,
  formatNodeVersion,
  formatRelativeTime,
  nodeHealth,
  PageHeader,
  PageTopBar,
  rowLinkClassName
} from "./operations-common";

type AdminRequest = <T>(path: string, init?: RequestInit) => Promise<T>;
type NodeFilter = "all" | "active" | "disabled" | "degraded";
type NodeSort = "name" | "status" | "public_host" | "last_seen_at" | "sing_box_version";
type SortDirection = "asc" | "desc";

function queryString(params: Record<string, string | number | undefined>) {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === "") continue;
    query.set(key, String(value));
  }
  const text = query.toString();
  return text ? `?${text}` : "";
}

function nodeStatusFilter(filter: NodeFilter): string | undefined {
  return filter === "all" ? undefined : filter;
}

function nodeTimestamp(node: AdminNode): string {
  return node.latest_heartbeat || node.last_seen_at;
}

function SortHead({
  label,
  column,
  sort,
  direction,
  setSort,
  className
}: {
  label: string;
  column: NodeSort;
  sort: NodeSort;
  direction: SortDirection;
  setSort: (column: NodeSort) => void;
  className?: string;
}) {
  const active = sort === column;
  const Icon = active && direction === "desc" ? SortDescendingIcon : SortAscendingIcon;
  return (
    <Table.Head className={className}>
      <button
        type="button"
        className="inline-flex items-center gap-1 text-left font-medium text-kumo-default hover:text-kumo-strong"
        onClick={() => setSort(column)}
      >
        {label}
        <Icon className={`size-3.5 ${active ? "text-kumo-default" : "text-kumo-subtle"}`} />
      </button>
    </Table.Head>
  );
}

function TableEmpty({ children }: { children: string }) {
  return (
    <Table.Row>
      <Table.Cell colSpan={8}>
        <div className="flex min-h-32 items-center justify-center text-sm text-kumo-subtle">{children}</div>
      </Table.Cell>
    </Table.Row>
  );
}

export function NodesPage({ request }: { request: AdminRequest }) {
  const [page, setPage] = useState(1);
  const [perPage, setPerPage] = useState(10);
  const [filter, setFilter] = useState<NodeFilter>("all");
  const [searchInput, setSearchInput] = useState("");
  const [search, setSearch] = useState("");
  const [sort, setSortValue] = useState<NodeSort>("name");
  const [direction, setDirection] = useState<SortDirection>("asc");

  function setSort(column: NodeSort) {
    setPage(1);
    if (sort === column) {
      setDirection((value) => (value === "asc" ? "desc" : "asc"));
      return;
    }
    setSortValue(column);
    setDirection(column === "last_seen_at" ? "desc" : "asc");
  }

  function setFilterValue(value: NodeFilter) {
    setFilter(value);
    setPage(1);
  }

  function setPageSize(value: number) {
    setPerPage(value);
    setPage(1);
  }

  const offset = (page - 1) * perPage;
  const path =
    "/api/admin/nodes" +
    queryString({
      limit: perPage,
      offset,
      search,
      status: nodeStatusFilter(filter),
      sort,
      direction
    });
  const nodesQuery = useQuery({
    queryKey: ["admin", "nodes-page", perPage, offset, search, filter, sort, direction],
    queryFn: () => request<AdminNodesResponse>(path),
    placeholderData: (previous) => previous
  });
  const pageData = nodesQuery.data;
  const nodes = pageData?.nodes ?? [];
  const total = pageData?.total ?? 0;
  const error = nodesQuery.error instanceof Error ? nodesQuery.error.message : "Request failed.";

  return (
    <div className="flex min-h-full flex-col bg-kumo-canvas">
      <PageTopBar current="Nodes" />
      <div className="relative z-[19] min-h-21 bg-kumo-canvas pb-2">
        <div className="mx-auto w-full max-w-[1400px] px-6 pt-3 pb-1 md:px-8 lg:px-10" />
      </div>
      <main className="w-full grow bg-kumo-canvas">
        <PageHeader
          title="Nodes"
          description="Operate edge nodes, config versions, heartbeats, and proxy placement."
          actions={
            <>
              <Button variant="secondary" shape="square" aria-label="Node actions">
                <DotsThreeIcon />
              </Button>
              <LinkButton href={adminPath("/nodes?create=1")} variant="primary" icon={PlusIcon}>
                Enroll
              </LinkButton>
            </>
          }
        />

        <div className="mx-auto flex w-full max-w-[1400px] flex-col gap-4 px-6 pb-8 md:px-8 lg:px-10">
          <section className="flex flex-col gap-3">
            <div className="flex flex-col gap-1 sm:flex-row sm:items-end sm:justify-between">
              <div>
                <h2 className="text-base font-semibold text-kumo-default">Node inventory</h2>
                <p className="text-sm text-kumo-subtle">
                  {total > 0 ? `Showing ${offset + 1}-${Math.min(offset + perPage, total)} of ${total}` : "No nodes"}
                </p>
              </div>
            </div>

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
                  placeholder="Search by node name, host, or status"
                  aria-label="Search nodes"
                  value={searchInput}
                  onChange={(event) => setSearchInput(event.target.value)}
                  className="min-w-0 flex-1"
                />
                <Button type="submit" variant="secondary">
                  Search
                </Button>
              </form>
              <DropdownMenu>
                <DropdownMenu.Trigger
                  render={
                    <Button variant="secondary" icon={FunnelIcon}>
                      Filter
                    </Button>
                  }
                />
                <DropdownMenu.Content>
                  <DropdownMenu.Group>
                    <DropdownMenu.Label>Status</DropdownMenu.Label>
                    <DropdownMenu.RadioGroup
                      value={filter}
                      onValueChange={(value) => setFilterValue(value as NodeFilter)}
                    >
                      <DropdownMenu.RadioItem value="all">
                        All
                        <DropdownMenu.RadioItemIndicator />
                      </DropdownMenu.RadioItem>
                      <DropdownMenu.RadioItem value="active">
                        Active
                        <DropdownMenu.RadioItemIndicator />
                      </DropdownMenu.RadioItem>
                      <DropdownMenu.RadioItem value="disabled">
                        Disabled
                        <DropdownMenu.RadioItemIndicator />
                      </DropdownMenu.RadioItem>
                      <DropdownMenu.RadioItem value="degraded">
                        Degraded
                        <DropdownMenu.RadioItemIndicator />
                      </DropdownMenu.RadioItem>
                    </DropdownMenu.RadioGroup>
                  </DropdownMenu.Group>
                </DropdownMenu.Content>
              </DropdownMenu>
            </div>

            <div className="overflow-hidden rounded-lg border border-kumo-line bg-kumo-base">
              <div className="overflow-x-auto">
                <Table>
                  <Table.Header variant="compact">
                    <Table.Row>
                      <SortHead label="Node" column="name" sort={sort} direction={direction} setSort={setSort} />
                      <SortHead label="Status" column="status" sort={sort} direction={direction} setSort={setSort} />
                      <SortHead label="Public host" column="public_host" sort={sort} direction={direction} setSort={setSort} />
                      <Table.Head>Agent</Table.Head>
                      <SortHead label="sing-box" column="sing_box_version" sort={sort} direction={direction} setSort={setSort} />
                      <Table.Head>Config</Table.Head>
                      <SortHead label="Last seen" column="last_seen_at" sort={sort} direction={direction} setSort={setSort} />
                      <Table.Head className="text-right">
                        <span className="sr-only">Actions</span>
                      </Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {nodesQuery.error ? (
                      <TableEmpty>{error}</TableEmpty>
                    ) : nodesQuery.isLoading ? (
                      <Table.Row>
                        <Table.Cell colSpan={8}>
                          <div className="flex min-h-32 items-center justify-center">
                            <Loader size={20} />
                          </div>
                        </Table.Cell>
                      </Table.Row>
                    ) : nodes.length > 0 ? (
                      nodes.map((node) => {
                        const health = nodeHealth(node);
                        const StatusIcon = health.icon;
                        const statusClassName = health.label === "Disabled" ? "text-kumo-subtle" : health.className;
                        return (
                          <Table.Row key={node.id}>
                            <Table.Cell>
                              <div className="flex min-w-48 items-center gap-2">
                                <HardDrivesIcon className="size-4 shrink-0 text-kumo-subtle" />
                                <Link href={adminPath(`/nodes?node=${encodeURIComponent(node.name)}`)} variant="current" className={rowLinkClassName}>
                                  <span className="truncate">{node.name}</span>
                                </Link>
                              </div>
                            </Table.Cell>
                            <Table.Cell>
                              <span className={`inline-flex items-center gap-1.5 whitespace-nowrap text-sm font-medium ${statusClassName}`}>
                                <StatusIcon className="size-4 shrink-0" />
                                {health.label}
                              </span>
                            </Table.Cell>
                            <Table.Cell>
                              <span className="whitespace-nowrap text-kumo-subtle">{node.public_host || node.api_base_url || "n/a"}</span>
                            </Table.Cell>
                            <Table.Cell>
                              <span className="whitespace-nowrap text-kumo-subtle">{node.agent_version || "n/a"}</span>
                            </Table.Cell>
                            <Table.Cell>
                              <span className="whitespace-nowrap text-kumo-subtle">{node.sing_box_version || "n/a"}</span>
                            </Table.Cell>
                            <Table.Cell>
                              <span className="whitespace-nowrap text-kumo-subtle">{formatNodeVersion(node)}</span>
                            </Table.Cell>
                            <Table.Cell>
                              <span className="whitespace-nowrap text-kumo-subtle">{formatRelativeTime(nodeTimestamp(node))}</span>
                            </Table.Cell>
                            <Table.Cell className="text-right">
                              <Button variant="ghost" size="sm" shape="square" aria-label={`Actions for ${node.name}`}>
                                <DotsThreeIcon className="size-4" />
                              </Button>
                            </Table.Cell>
                          </Table.Row>
                        );
                      })
                    ) : (
                      <TableEmpty>No nodes match this filter.</TableEmpty>
                    )}
                  </Table.Body>
                </Table>
              </div>
            </div>

            <Pagination page={page} setPage={setPage} perPage={perPage} totalCount={total} className="mt-1">
              <Pagination.Info>
                {({ pageShowingRange, totalCount }) => (
                  <span>
                    <strong>{pageShowingRange}</strong> of {totalCount} items
                  </span>
                )}
              </Pagination.Info>
              <Pagination.Separator />
              <Pagination.PageSize value={perPage} onChange={setPageSize} options={[10, 25, 50, 100]} label="Items per page:" />
              <Pagination.Controls controls="simple" />
            </Pagination>
          </section>
        </div>
      </main>
    </div>
  );
}
