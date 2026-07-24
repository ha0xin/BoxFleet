import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  CheckCircleIcon,
  DotsThreeIcon,
  FunnelIcon,
  PathIcon,
  PencilSimpleIcon,
  PlusIcon,
  ProhibitIcon,
  TrashIcon,
  XCircleIcon
} from "@phosphor-icons/react";
import { Button, DropdownMenu, Input, Table } from "@cloudflare/kumo";

import type { AdminProxy, AdminProxiesResponse } from "../types";
import { formatRelativeTime, PageHeader, PageTopBar } from "./operations-common";
import { useAdminMutation } from "@/admin/use-admin-mutation";
import { useAdminApi } from "@/admin/api";
import { adminKeys, queryString } from "@/admin/query";
import { AdminPagination, SortHead, TableEmpty, TableLoading } from "@/components/admin-table";
import { ProxyFormDialog } from "./proxy-dialogs";
import type { ProxyDialogState } from "./proxy-dialogs";
import { SoftDeleteDialog } from "./soft-delete-dialog";

type ProxyFilter = "all" | "enabled" | "disabled" | "deleted";
type ProxySort = "node_name" | "name" | "protocol" | "listen_port" | "enabled" | "traffic_multiplier" | "updated_at";
type SortDirection = "asc" | "desc";

function proxyEnabledFilter(filter: ProxyFilter): string | undefined {
  if (filter === "enabled") return "true";
  if (filter === "disabled") return "false";
  return undefined;
}

function proxyStatus(proxy: AdminProxy) {
  if (proxy.deleted_at) {
    return { label: "Deleted", icon: XCircleIcon, className: "text-kumo-subtle" };
  }
  if (!proxy.enabled) {
    return { label: "Disabled", icon: XCircleIcon, className: "text-kumo-subtle" };
  }
  return { label: "Enabled", icon: CheckCircleIcon, className: "text-kumo-success" };
}

function endpoint(proxy: AdminProxy): string {
  const listen = proxy.listen === "::" || proxy.listen === "0.0.0.0" ? "*" : proxy.listen;
  return `${listen}:${proxy.listen_port}`;
}

function multiplier(value: number): string {
  return `${Number.isInteger(value) ? value.toFixed(0) : value.toFixed(1)}x`;
}

export function ProxiesPage() {
  const { request } = useAdminApi();
  const [page, setPage] = useState(1);
  const [perPage, setPerPage] = useState(10);
  const [filter, setFilter] = useState<ProxyFilter>("all");
  const [searchInput, setSearchInput] = useState("");
  const [search, setSearch] = useState("");
  const [sort, setSortValue] = useState<ProxySort>("node_name");
  const [direction, setDirection] = useState<SortDirection>("asc");
  const [dialog, setDialog] = useState<ProxyDialogState>(null);
  const [deleteTarget, setDeleteTarget] = useState<AdminProxy | null>(null);

  const toggleEnabled = useAdminMutation<AdminProxy>(request, (req, proxy) =>
    req(`/api/admin/nodes/${encodeURIComponent(proxy.node_name)}/proxies/${encodeURIComponent(proxy.name)}`, {
      method: "PATCH",
      body: JSON.stringify({ enabled: !proxy.enabled })
    })
  );
  const restore = useAdminMutation<AdminProxy>(request, (req, proxy) =>
    req(
      `/api/admin/nodes/${encodeURIComponent(proxy.node_name)}/proxies/${encodeURIComponent(proxy.name)}/restore`,
      { method: "POST" }
    )
  );

  function setSort(column: ProxySort) {
    setPage(1);
    if (sort === column) {
      setDirection((value) => (value === "asc" ? "desc" : "asc"));
      return;
    }
    setSortValue(column);
    setDirection(column === "updated_at" ? "desc" : "asc");
  }

  function setFilterValue(value: ProxyFilter) {
    setFilter(value);
    setPage(1);
  }

  function setPageSize(value: number) {
    setPerPage(value);
    setPage(1);
  }

  const offset = (page - 1) * perPage;
  const path =
    "/api/admin/proxies" +
    queryString({
      limit: perPage,
      offset,
      search,
      enabled: proxyEnabledFilter(filter),
      deleted: filter === "deleted" ? "true" : undefined,
      sort,
      direction
    });
  const proxiesQuery = useQuery({
    queryKey: adminKeys.proxiesPage(perPage, offset, search, filter, sort, direction),
    queryFn: () => request<AdminProxiesResponse>(path),
    placeholderData: (previous) => previous
  });
  const pageData = proxiesQuery.data;
  const proxies = pageData?.proxies ?? [];
  const total = pageData?.total ?? 0;
  const error = proxiesQuery.error instanceof Error ? proxiesQuery.error.message : "Request failed.";

  const lastPage = Math.max(1, Math.ceil(total / perPage));
  if (page > lastPage) setPage(lastPage);

  return (
    <div className="flex min-h-full flex-col bg-kumo-canvas">
      <PageTopBar current="Proxies" />
      <main className="w-full grow bg-kumo-canvas">
        <PageHeader
          title="Proxies"
          description="Review VLESS-Reality inbounds, ports, routing rules, and node placement."
          actions={
            <Button variant="primary" icon={PlusIcon} onClick={() => setDialog({ mode: "create" })}>
              Create
            </Button>
          }
        />

        <div className="mx-auto flex w-full max-w-[1400px] flex-col gap-4 px-6 pb-8 md:px-8 lg:px-10">
          <section className="flex flex-col gap-3">
            <div className="flex flex-col gap-1 sm:flex-row sm:items-end sm:justify-between">
              <div>
                <h2 className="text-base font-semibold text-kumo-default">Proxy inventory</h2>
                <p className="text-sm text-kumo-subtle">
                  {total > 0 ? `Showing ${offset + 1}-${Math.min(offset + perPage, total)} of ${total}` : "No proxies"}
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
                  placeholder="Search by proxy, node, protocol, or port"
                  aria-label="Search proxies"
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
                      onValueChange={(value) => setFilterValue(value as ProxyFilter)}
                    >
                      <DropdownMenu.RadioItem value="all">
                        All
                        <DropdownMenu.RadioItemIndicator />
                      </DropdownMenu.RadioItem>
                      <DropdownMenu.RadioItem value="enabled">
                        Enabled
                        <DropdownMenu.RadioItemIndicator />
                      </DropdownMenu.RadioItem>
                      <DropdownMenu.RadioItem value="disabled">
                        Disabled
                        <DropdownMenu.RadioItemIndicator />
                      </DropdownMenu.RadioItem>
                      <DropdownMenu.RadioItem value="deleted">
                        Deleted
                        <DropdownMenu.RadioItemIndicator />
                      </DropdownMenu.RadioItem>
                    </DropdownMenu.RadioGroup>
                  </DropdownMenu.Group>
                </DropdownMenu.Content>
              </DropdownMenu>
            </div>

            <div className="overflow-hidden rounded-lg border border-kumo-line bg-kumo-base">
              <div className="bf-table-scroll overflow-x-auto overscroll-x-contain">
                <Table className="min-w-[1280px]">
                  <Table.Header variant="compact">
                    <Table.Row>
                      <SortHead label="Proxy" column="name" sort={sort} direction={direction} setSort={setSort} className="sticky left-0 z-20 bg-kumo-base" />
                      <SortHead label="Node" column="node_name" sort={sort} direction={direction} setSort={setSort} />
                      <SortHead label="Status" column="enabled" sort={sort} direction={direction} setSort={setSort} />
                      <SortHead label="Protocol" column="protocol" sort={sort} direction={direction} setSort={setSort} />
                      <Table.Head>Listen</Table.Head>
                      <SortHead label="Port" column="listen_port" sort={sort} direction={direction} setSort={setSort} />
                      <Table.Head>Transport</Table.Head>
                      <SortHead label="Multiplier" column="traffic_multiplier" sort={sort} direction={direction} setSort={setSort} />
                      <SortHead label="Updated" column="updated_at" sort={sort} direction={direction} setSort={setSort} />
                      <Table.Head className="text-right">
                        <span className="sr-only">Actions</span>
                      </Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {proxiesQuery.error ? (
                      <TableEmpty colSpan={10}>{error}</TableEmpty>
                    ) : proxiesQuery.isLoading ? (
                      <TableLoading colSpan={10} />
                    ) : proxies.length > 0 ? (
                      proxies.map((proxy) => {
                        const status = proxyStatus(proxy);
                        const StatusIcon = status.icon;
                        return (
                          <Table.Row key={proxy.id}>
                            <Table.Cell className="sticky left-0 z-10 bg-kumo-base">
                              <div className="flex min-w-48 items-center gap-2">
                                <PathIcon className="size-4 shrink-0 text-kumo-subtle" />
                                <span className="truncate text-base font-medium text-kumo-default" title={proxy.name}>{proxy.name}</span>
                              </div>
                            </Table.Cell>
                            <Table.Cell>
                              <span className="whitespace-nowrap text-kumo-subtle">{proxy.node_name}</span>
                            </Table.Cell>
                            <Table.Cell>
                              <span className={`inline-flex items-center gap-1.5 whitespace-nowrap text-sm font-medium ${status.className}`}>
                                <StatusIcon className="size-4 shrink-0" />
                                {status.label}
                              </span>
                            </Table.Cell>
                            <Table.Cell>
                              <span className="whitespace-nowrap text-kumo-subtle">{proxy.protocol}</span>
                            </Table.Cell>
                            <Table.Cell>
                              <span className="whitespace-nowrap text-kumo-subtle">{endpoint(proxy)}</span>
                            </Table.Cell>
                            <Table.Cell>
                              <span className="whitespace-nowrap text-kumo-subtle">{proxy.listen_port}</span>
                            </Table.Cell>
                            <Table.Cell>
                              <span className="whitespace-nowrap text-kumo-subtle">{proxy.transport}</span>
                            </Table.Cell>
                            <Table.Cell>
                              <span className="whitespace-nowrap text-kumo-subtle">{multiplier(proxy.traffic_multiplier)}</span>
                            </Table.Cell>
                            <Table.Cell>
                              <span className="whitespace-nowrap text-kumo-subtle">{formatRelativeTime(proxy.updated_at)}</span>
                            </Table.Cell>
                            <Table.Cell className="text-right">
                              <DropdownMenu>
                                <DropdownMenu.Trigger
                                  render={
                                    <Button variant="ghost" size="sm" shape="square" aria-label={`Actions for ${proxy.name}`}>
                                      <DotsThreeIcon className="size-4" />
                                    </Button>
                                  }
                                />
                                <DropdownMenu.Content>
                                  {proxy.deleted_at ? (
                                    <DropdownMenu.Item icon={CheckCircleIcon} onClick={() => restore.mutate(proxy)}>
                                      Restore
                                    </DropdownMenu.Item>
                                  ) : (
                                    <>
                                      <DropdownMenu.Item icon={PencilSimpleIcon} onClick={() => setDialog({ mode: "edit", proxy })}>
                                        Edit
                                      </DropdownMenu.Item>
                                      <DropdownMenu.Item
                                        icon={proxy.enabled ? ProhibitIcon : CheckCircleIcon}
                                        onClick={() => toggleEnabled.mutate(proxy)}
                                      >
                                        {proxy.enabled ? "Disable" : "Enable"}
                                      </DropdownMenu.Item>
                                      <DropdownMenu.Separator />
                                      <DropdownMenu.Item variant="danger" icon={TrashIcon} onClick={() => setDeleteTarget(proxy)}>
                                        Delete
                                      </DropdownMenu.Item>
                                    </>
                                  )}
                                </DropdownMenu.Content>
                              </DropdownMenu>
                            </Table.Cell>
                          </Table.Row>
                        );
                      })
                    ) : (
                      <TableEmpty colSpan={10}>No proxies match this filter.</TableEmpty>
                    )}
                  </Table.Body>
                </Table>
              </div>
            </div>

            <AdminPagination page={page} setPage={setPage} perPage={perPage} setPerPage={setPageSize} total={total} />
          </section>
        </div>
      </main>

      {dialog?.mode === "create" || dialog?.mode === "edit" ? (
        <ProxyFormDialog request={request} state={dialog} onClose={() => setDialog(null)} />
      ) : null}
      {deleteTarget ? (
        <SoftDeleteDialog
          request={request}
          title="Delete proxy"
          description={
            <>
              Delete <span className="font-medium text-kumo-default">{deleteTarget.name}</span>? It will disappear from the default inventory and can be restored from the Deleted filter.
            </>
          }
          endpoint={`/api/admin/nodes/${encodeURIComponent(deleteTarget.node_name)}/proxies/${encodeURIComponent(deleteTarget.name)}`}
          onClose={() => setDeleteTarget(null)}
        />
      ) : null}
    </div>
  );
}
