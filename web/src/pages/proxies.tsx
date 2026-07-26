import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowsClockwiseIcon,
  CheckCircleIcon,
  DotsThreeIcon,
  FunnelIcon,
  PathIcon,
  PencilSimpleIcon,
  PlusIcon,
  ProhibitIcon,
  TrashIcon
} from "@phosphor-icons/react";
import { Badge, Button, DropdownMenu, Input, Table } from "@cloudflare/kumo";

import type { AdminProxy, AdminProxiesResponse } from "../types";
import { formatRelativeTime } from "./operations-common";
import { useAdminMutation } from "@/admin/use-admin-mutation";
import { useAdminApi } from "@/admin/api";
import { adminKeys, queryString } from "@/admin/query";
import { AppPageHeader } from "@/components/app-page-header";
import { StatusBadge } from "@/components/status-badge";
import { AdminPagination, SortHead, TableCard, TableEmpty, TableError, TableLoading } from "@/components/admin-table";
import { ProxyFormDialog } from "./proxy-dialogs";
import type { ProxyDialogState } from "./proxy-dialogs";
import { SoftDeleteDialog } from "./soft-delete-dialog";

type ProxyFilter = "all" | "enabled" | "disabled" | "deleted";
type ProxySort = "node_name" | "name" | "protocol" | "listen_port" | "enabled" | "traffic_multiplier" | "updated_at";
type SortDirection = "asc" | "desc";

const FILTER_LABELS: Record<Exclude<ProxyFilter, "all">, string> = {
  enabled: "Enabled",
  disabled: "Disabled",
  deleted: "Deleted"
};

function proxyEnabledFilter(filter: ProxyFilter): string | undefined {
  if (filter === "enabled") return "true";
  if (filter === "disabled") return "false";
  return undefined;
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

  const lastPage = Math.max(1, Math.ceil(total / perPage));
  // Render-phase adjustment (react.dev "you might not need an effect"):
  // clamp when the row count shrinks below the current page.
  if (page > lastPage) setPage(lastPage);

  return (
    <div className="flex min-h-full flex-col bg-kumo-canvas">
      <AppPageHeader
        title="Proxies"
        description="Review VLESS-Reality and Shadowsocks 2022 inbounds, ports, and node placement."
        actions={
          <Button variant="primary" icon={PlusIcon} onClick={() => setDialog({ mode: "create" })}>
            Create
          </Button>
        }
      />
      <main className="w-full grow bg-kumo-canvas">
        <div className="mx-auto flex w-full max-w-[1400px] flex-col gap-4 px-6 pb-8 md:px-8 lg:px-10">
          <section className="flex flex-col gap-3">
            <div>
              <h2 className="text-base font-semibold text-kumo-default">Proxy inventory</h2>
              <p className="text-sm text-kumo-subtle">
                {total === 0 ? "No proxies" : `${total} ${total === 1 ? "proxy" : "proxies"}`}
              </p>
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
                      {filter !== "all" ? <Badge variant="secondary">{FILTER_LABELS[filter]}</Badge> : null}
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

            <TableCard>
              <Table className="min-w-[1280px]">
                <Table.Header variant="compact">
                  <Table.Row>
                    <SortHead label="Proxy" column="name" sort={sort} direction={direction} setSort={setSort} sticky="left" />
                    <SortHead label="Node" column="node_name" sort={sort} direction={direction} setSort={setSort} />
                    <SortHead label="Status" column="enabled" sort={sort} direction={direction} setSort={setSort} />
                    <SortHead label="Protocol" column="protocol" sort={sort} direction={direction} setSort={setSort} />
                    <SortHead label="Listen" column="listen_port" sort={sort} direction={direction} setSort={setSort} />
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
                    <TableError colSpan={9}>
                      {proxiesQuery.error instanceof Error ? proxiesQuery.error.message : "Request failed."}
                    </TableError>
                  ) : proxiesQuery.isLoading ? (
                    <TableLoading colSpan={9} />
                  ) : proxies.length > 0 ? (
                    proxies.map((proxy) => (
                      <Table.Row key={proxy.id}>
                        <Table.Cell sticky="left">
                          <div className="flex min-w-48 items-center gap-2">
                            <PathIcon className="size-4 shrink-0 text-kumo-subtle" />
                            <span className="truncate text-base font-medium text-kumo-default" title={proxy.name}>{proxy.name}</span>
                          </div>
                        </Table.Cell>
                        <Table.Cell>
                          <span className="whitespace-nowrap text-kumo-subtle">{proxy.node_name}</span>
                        </Table.Cell>
                        <Table.Cell>
                          <StatusBadge tone={proxy.deleted_at ? "error" : proxy.enabled ? "success" : "neutral"}>
                            {proxy.deleted_at ? "Deleted" : proxy.enabled ? "Enabled" : "Disabled"}
                          </StatusBadge>
                        </Table.Cell>
                        <Table.Cell>
                          <span className="whitespace-nowrap text-kumo-subtle">{proxy.protocol}</span>
                        </Table.Cell>
                        <Table.Cell>
                          <span className="whitespace-nowrap text-kumo-subtle">{endpoint(proxy)}</span>
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
                                <DropdownMenu.Item
                                  icon={ArrowsClockwiseIcon}
                                  disabled={restore.isPending}
                                  onClick={() => restore.mutate(proxy)}
                                >
                                  Restore
                                </DropdownMenu.Item>
                              ) : (
                                <>
                                  <DropdownMenu.Item icon={PencilSimpleIcon} onClick={() => setDialog({ mode: "edit", proxy })}>
                                    Edit
                                  </DropdownMenu.Item>
                                  <DropdownMenu.Item
                                    icon={proxy.enabled ? ProhibitIcon : CheckCircleIcon}
                                    disabled={toggleEnabled.isPending}
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
                    ))
                  ) : (
                    <TableEmpty colSpan={9}>No proxies match this filter.</TableEmpty>
                  )}
                </Table.Body>
              </Table>
            </TableCard>

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
