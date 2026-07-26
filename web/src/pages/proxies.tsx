import { useEffect, useState } from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import { useQuery } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { z } from "zod";
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
import { adminKeys, queryString, refreshIntervals } from "@/admin/query";
import { useUrlFilters, type UseUrlFiltersOptions } from "@/admin/use-url-filters";
import { AppPageHeader } from "@/components/app-page-header";
import { StatusBadge } from "@/components/status-badge";
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
import { ProxyFormDialog } from "./proxy-dialogs";
import type { ProxyDialogState } from "./proxy-dialogs";
import { SoftDeleteDialog } from "./soft-delete-dialog";

/**
 * The inventory has always been paged server-side (`ListProxiesPage`); what it
 * lacked was a linkable address for the view. Filters, sort and pagination now
 * live in the query string via `useUrlFilters`, so a refresh, a Back press or a
 * pasted link all reproduce the same rows.
 */
const filterSchema = z.object({
  search: z.string(),
  status: z.enum(["all", "enabled", "disabled", "deleted"]),
  sort: z.enum(["node_name", "name", "protocol", "listen_port", "enabled", "traffic_multiplier", "updated_at"]),
  direction: z.enum(["asc", "desc"])
});

/**
 * Column widths, in table order. Everything with a bounded vocabulary — the
 * status badge, the protocol and transport names, the multiplier, the relative
 * timestamp, the kebab — is pinned to its measured max-content width. Only the
 * three columns built from operator-chosen strings flex.
 */
const proxyColumns: TableColumnWidth[] = [
  { min: 116 }, // Proxy
  { min: 116 }, // Node
  104, // Status
  112, // Protocol
  { min: 116 }, // Listen
  104, // Transport
  108, // Multiplier
  108, // Updated
  52 // Actions
];

type ProxyFilterValues = z.infer<typeof filterSchema>;
type ProxyStatus = ProxyFilterValues["status"];
type ProxySort = ProxyFilterValues["sort"];

const defaultFilters: ProxyFilterValues = {
  search: "",
  status: "all",
  sort: "node_name",
  direction: "asc"
};

// Module scope: the hook re-parses on every render, and a fresh options object
// would break the referential stability of the returned `filters`.
const urlFilters: UseUrlFiltersOptions<ProxyFilterValues> = {
  schema: filterSchema,
  defaults: defaultFilters,
  perPage: 10
};

const FILTER_LABELS: Record<Exclude<ProxyStatus, "all">, string> = {
  enabled: "Enabled",
  disabled: "Disabled",
  deleted: "Deleted"
};

/** Columns that read best newest-first when first selected. */
const DESCENDING_FIRST: ReadonlySet<ProxySort> = new Set<ProxySort>(["updated_at"]);

/**
 * The one radio group covers two independent server filters: `enabled` narrows
 * live rows, `deleted=true` swaps the base set to soft-deleted ones. Keeping the
 * mapping in a pure function keeps that split testable.
 */
export function proxyPageParams(filters: ProxyFilterValues, limit: number, offset: number) {
  return {
    limit,
    offset,
    search: filters.search,
    enabled: filters.status === "enabled" ? "true" : filters.status === "disabled" ? "false" : undefined,
    deleted: filters.status === "deleted" ? "true" : undefined,
    sort: filters.sort,
    direction: filters.direction
  };
}

export function endpoint(proxy: AdminProxy): string {
  const listen = proxy.listen === "::" || proxy.listen === "0.0.0.0" ? "*" : proxy.listen;
  return `${listen}:${proxy.listen_port}`;
}

export function multiplier(value: number): string {
  return `${Number.isInteger(value) ? value.toFixed(0) : value.toFixed(1)}x`;
}

export function ProxiesPage() {
  const { request } = useAdminApi();
  const { filters, page, perPage, offset, setFilters, setPage, setPerPage } = useUrlFilters(urlFilters);
  const [dialog, setDialog] = useState<ProxyDialogState>(null);
  const [deleteTarget, setDeleteTarget] = useState<AdminProxy | null>(null);

  // react-hook-form owns the uncommitted search text; `values` re-syncs the box
  // when the URL changes underneath it (Back/Forward, or a pasted link).
  const form = useForm<ProxyFilterValues>({ resolver: zodResolver(filterSchema), values: filters });

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

  // The updater form reads the committed filters rather than this render's
  // closure, and the hook resets to page 1 for free.
  function setSort(column: ProxySort) {
    setFilters((current) =>
      current.sort === column
        ? { direction: current.direction === "asc" ? "desc" : "asc" }
        : { sort: column, direction: DESCENDING_FIRST.has(column) ? "desc" : "asc" }
    );
  }

  const path = "/api/admin/proxies" + queryString(proxyPageParams(filters, perPage, offset));
  const proxiesQuery = useQuery({
    queryKey: adminKeys.proxiesPage(perPage, offset, filters.search, filters.status, filters.sort, filters.direction),
    queryFn: ({ signal }) => request<AdminProxiesResponse>(path, { signal }),
    placeholderData: (previous) => previous,
    // Proxy status is derived from node heartbeats, so it moves on its own.
    refetchInterval: refreshIntervals.live
  });
  const proxies = proxiesQuery.data?.proxies ?? [];
  const total = proxiesQuery.data?.total ?? 0;

  // The hook never sees a row count, so the upper clamp lives here. Two details
  // matter: it must run in an effect (writing search params during render is a
  // navigation), and it must wait for a real total — clamping against the
  // pre-fetch `total ?? 0` would rewrite `?page=3` to page 1 on every cold load
  // of a deep link, before the server ever said the page was out of range.
  const loadedTotal = proxiesQuery.data?.total;
  useEffect(() => {
    if (loadedTotal === undefined) return;
    const lastPage = Math.max(1, Math.ceil(loadedTotal / perPage));
    if (page > lastPage) setPage(lastPage, "replace");
  }, [loadedTotal, page, perPage, setPage]);

  return (
    // `min-w-0`: this div is a grid item, and without it the table's min-width
    // becomes the page's min-width and the whole page scrolls sideways instead
    // of the table.
    <div className="flex min-h-full min-w-0 flex-col bg-kumo-canvas">
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
                onSubmit={form.handleSubmit((values) => setFilters({ search: values.search.trim() }))}
              >
                <Input
                  placeholder="Search by proxy, node, protocol, or port"
                  aria-label="Search proxies"
                  className="min-w-0 flex-1"
                  {...form.register("search")}
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
                      {filters.status !== "all" ? (
                        <Badge variant="secondary">{FILTER_LABELS[filters.status]}</Badge>
                      ) : null}
                    </Button>
                  }
                />
                <DropdownMenu.Content>
                  <DropdownMenu.Group>
                    <DropdownMenu.Label>Status</DropdownMenu.Label>
                    <DropdownMenu.RadioGroup
                      value={filters.status}
                      onValueChange={(value) => setFilters({ status: value as ProxyStatus })}
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
              <Table layout="fixed" style={{ minWidth: tableMinWidth(proxyColumns) }}>
                <TableColgroup widths={proxyColumns} />
                <Table.Header variant="compact">
                  <Table.Row>
                    <SortHead label="Proxy" column="name" sort={filters.sort} direction={filters.direction} setSort={setSort} sticky="left" />
                    <SortHead label="Node" column="node_name" sort={filters.sort} direction={filters.direction} setSort={setSort} />
                    <SortHead label="Status" column="enabled" sort={filters.sort} direction={filters.direction} setSort={setSort} />
                    <SortHead label="Protocol" column="protocol" sort={filters.sort} direction={filters.direction} setSort={setSort} />
                    <SortHead label="Listen" column="listen_port" sort={filters.sort} direction={filters.direction} setSort={setSort} />
                    <Table.Head>Transport</Table.Head>
                    <SortHead label="Multiplier" column="traffic_multiplier" sort={filters.sort} direction={filters.direction} setSort={setSort} />
                    <SortHead label="Updated" column="updated_at" sort={filters.sort} direction={filters.direction} setSort={setSort} />
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
                          <div className="flex min-w-0 items-center gap-2">
                            <PathIcon className="size-4 shrink-0 text-kumo-subtle" />
                            <span className="truncate text-base font-medium text-kumo-default" title={proxy.name}>{proxy.name}</span>
                          </div>
                        </Table.Cell>
                        <Table.Cell>
                          <span className="block truncate text-kumo-subtle" title={proxy.node_name}>{proxy.node_name}</span>
                        </Table.Cell>
                        <Table.Cell>
                          <StatusBadge tone={proxy.deleted_at ? "error" : proxy.enabled ? "success" : "neutral"}>
                            {proxy.deleted_at ? "Deleted" : proxy.enabled ? "Enabled" : "Disabled"}
                          </StatusBadge>
                        </Table.Cell>
                        <Table.Cell>
                          <span className="block truncate text-kumo-subtle" title={proxy.protocol}>{proxy.protocol}</span>
                        </Table.Cell>
                        <Table.Cell>
                          <span className="block truncate text-kumo-subtle" title={endpoint(proxy)}>{endpoint(proxy)}</span>
                        </Table.Cell>
                        <Table.Cell>
                          <span className="block truncate text-kumo-subtle" title={proxy.transport}>{proxy.transport}</span>
                        </Table.Cell>
                        <Table.Cell>
                          <span className="block truncate text-kumo-subtle">{multiplier(proxy.traffic_multiplier)}</span>
                        </Table.Cell>
                        <Table.Cell>
                          <span className="block truncate text-kumo-subtle" title={proxy.updated_at || undefined}>
                            {formatRelativeTime(proxy.updated_at)}
                          </span>
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

            <AdminPagination page={page} setPage={setPage} perPage={perPage} setPerPage={setPerPage} total={total} />
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
