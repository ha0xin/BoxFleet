import type { CSSProperties } from "react";
import { useEffect, useState } from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import { useQuery } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { z } from "zod";
import {
  ArrowsClockwiseIcon,
  CheckCircleIcon,
  FunnelIcon,
  IdentificationCardIcon,
  KeyIcon,
  PencilSimpleIcon,
  PlusIcon,
  ProhibitIcon,
  TrashIcon,
  UserIcon
} from "@phosphor-icons/react";
import { Badge, Button, DropdownMenu, Input, Meter, Table } from "@cloudflare/kumo";

import type { AdminUser, AdminUserEffectiveStatus, AdminUsersResponse, TrafficVolume } from "../types";
import { formatBytes } from "../utils";
import { useAdminMutation } from "@/admin/use-admin-mutation";
import { useAdminApi } from "@/admin/api";
import { adminKeys, queryString, refreshIntervals } from "@/admin/query";
import { useUrlFilters, type UseUrlFiltersOptions } from "@/admin/use-url-filters";
import { ConnectionInfoDialog, ManageAccessDialog, UserFormDialog } from "./user-dialogs";
import type { UserDialogState } from "./user-dialogs";
import { SoftDeleteDialog } from "./soft-delete-dialog";
import { AppPageHeader } from "@/components/app-page-header";
import { RowActionsMenu } from "@/components/row-actions-menu";
import { StatusBadge } from "@/components/status-badge";
import type { StatusTone } from "@/components/status-badge";
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

/**
 * Status vocabulary of the paged endpoint. These are the *derived* statuses the
 * server filters and sorts on (`effective_status`), not the stored column: a
 * user whose quota ran out is `quota_exceeded` here long before anything writes
 * that word to `proxy_users.status`.
 */
const STATUS_LABELS: Record<AdminUserEffectiveStatus, string> = {
  active: "Active",
  disabled: "Disabled",
  expired: "Expired",
  quota_exceeded: "Over quota",
  deleted: "Deleted"
};

const STATUS_TONES: Record<AdminUserEffectiveStatus, StatusTone> = {
  active: "success",
  disabled: "neutral",
  expired: "warning",
  quota_exceeded: "warning",
  deleted: "error"
};

const STATUS_FILTERS = ["all", "active", "disabled", "expired", "quota_exceeded", "deleted"] as const;

const FILTER_LABELS: Record<(typeof STATUS_FILTERS)[number], string> = { all: "All", ...STATUS_LABELS };

const filterSchema = z.object({
  search: z.string(),
  status: z.enum(STATUS_FILTERS),
  // Only the keys the server whitelists; anything else it would silently sort
  // by name, which would leave the header arrow pointing at a lie.
  sort: z.enum(["name", "status", "traffic", "quota", "proxy_count", "expire_at"]),
  direction: z.enum(["asc", "desc"])
});

export type UserFilterValues = z.infer<typeof filterSchema>;

type UserSort = UserFilterValues["sort"];

const defaultFilters: UserFilterValues = { search: "", status: "all", sort: "name", direction: "asc" };

// Module scope keeps `filters` referentially stable across renders, which is
// what lets it be spread straight into a query key.
const urlFilters: UseUrlFiltersOptions<UserFilterValues> = {
  schema: filterSchema,
  defaults: defaultFilters,
  perPage: 10
};

/**
 * Request path for one page of users.
 *
 * "Deleted" sits in the same menu as the other statuses but is a different axis
 * on the wire: `status` narrows the derived status *within* the live inventory,
 * while `deleted=true` swaps the inventory itself to the soft-deleted rows.
 * Sending both would ask for deleted users that are not deleted — always empty.
 */
export function usersRequestPath(filters: UserFilterValues, perPage: number, offset: number): string {
  return "/api/admin/users" + queryString({
    limit: perPage,
    offset,
    search: filters.search.trim(),
    status: filters.status === "all" || filters.status === "deleted" ? undefined : filters.status,
    deleted: filters.status === "deleted" ? "true" : undefined,
    sort: filters.sort,
    direction: filters.direction
  });
}

/** Billable bytes are what a quota is measured against. */
export function billableBytes(traffic: TrafficVolume): number {
  return traffic.uplink_billable_bytes + traffic.downlink_billable_bytes;
}

/** Raw bytes are what crossed the wire, before any per-proxy multiplier. */
export function rawBytes(traffic: TrafficVolume): number {
  return traffic.uplink_raw_bytes + traffic.downlink_raw_bytes;
}

/**
 * Column widths, in table order. The quota meter and the user identity are the
 * only cells whose useful width is open-ended, so they take the leftover space;
 * a status badge, a byte figure, a grant count and a relative expiry all have a
 * hard content ceiling and are pinned to it.
 */
const userColumns: TableColumnWidth[] = [
  { min: 192 }, // User
  120, // Status
  116, // Traffic
  { min: 192 }, // Quota — the meter plus its up/down legend
  96, // Access
  116, // Expires
  52 // Actions
];

export function formatExpiry(value: string): string {
  if (!value) return "never";
  const time = new Date(value).getTime();
  if (!Number.isFinite(time)) return value;
  const seconds = Math.round((time - Date.now()) / 1000);
  const abs = Math.abs(seconds);
  const suffix = seconds >= 0 ? "" : " ago";
  const prefix = seconds >= 0 ? "in " : "";
  if (abs < 60) return seconds >= 0 ? "soon" : "expired";
  const minutes = Math.floor(abs / 60);
  if (minutes < 60) return `${prefix}${minutes}m${suffix}`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${prefix}${hours}h${suffix}`;
  const days = Math.floor(hours / 24);
  return `${prefix}${days}d${suffix}`;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "Request failed.";
}

function QuotaMeter({ quota, traffic }: { quota: number; traffic: TrafficVolume }) {
  const total = billableBytes(traffic);
  const max = quota > 0 ? quota : Math.max(total, 1);
  const value = quota > 0 ? Math.min(total, quota) : total;
  const uploadShare = total > 0 ? Math.round((traffic.uplink_billable_bytes / total) * 100) : 0;
  const style = {
    "--meter-split": `${uploadShare}%`
  } as CSSProperties;

  return (
    <div className="min-w-0">
      <Meter
        label="Usage"
        value={value}
        max={max}
        customValue={quota > 0 ? `${formatBytes(total)} / ${formatBytes(quota)}` : `${formatBytes(total)} total`}
        showValue={false}
        style={style}
        indicatorClassName="![background:linear-gradient(to_right,var(--color-kumo-info)_0_var(--meter-split),var(--color-kumo-success)_var(--meter-split)_100%)]"
      />
      <div className="mt-1 flex items-center gap-3 text-xs text-kumo-subtle">
        <span className="inline-flex items-center gap-1">
          <span className="size-2 rounded-full bg-kumo-info" />
          {formatBytes(traffic.uplink_billable_bytes)} up
        </span>
        <span className="inline-flex items-center gap-1">
          <span className="size-2 rounded-full bg-kumo-success" />
          {formatBytes(traffic.downlink_billable_bytes)} down
        </span>
      </div>
    </div>
  );
}

export function UsersPage() {
  const { request } = useAdminApi();
  const { filters, page, perPage, offset, setFilters, setPage, setPerPage } = useUrlFilters(urlFilters);
  const [dialog, setDialog] = useState<UserDialogState>(null);
  const [deleteTarget, setDeleteTarget] = useState<AdminUser | null>(null);

  // react-hook-form is the draft layer for the search box only; `values` re-syncs
  // it whenever the committed filters change, including on Back/Forward.
  const form = useForm<UserFilterValues>({ resolver: zodResolver(filterSchema), values: filters });

  const toggleStatus = useAdminMutation<AdminUser>(request, (req, user) =>
    req(`/api/admin/users/${encodeURIComponent(user.name)}`, {
      method: "PATCH",
      body: JSON.stringify({ status: user.status === "disabled" ? "active" : "disabled" })
    })
  );
  const restore = useAdminMutation<AdminUser>(request, (req, user) =>
    req(`/api/admin/users/${encodeURIComponent(user.name)}/restore`, { method: "POST" })
  );

  // One request per page carries the rows, the derived status and the traffic
  // totals. The page used to pair the full user list with the fleet-wide
  // /api/admin/traffic/users inventory and join them by name in the client;
  // under server-side paging that join is unfixable — the server has to filter
  // and sort by traffic before it knows which ten rows to send, so the numbers
  // must come from the same query. The 15s cadence is the tighter of the two
  // the page used to run, and it is now a page of rows instead of two full
  // inventories.
  const usersQuery = useQuery({
    queryKey: adminKeys.usersPage(perPage, offset, filters.search, filters.status, filters.sort, filters.direction),
    queryFn: ({ signal }) => request<AdminUsersResponse>(usersRequestPath(filters, perPage, offset), { signal }),
    placeholderData: (previous) => previous,
    refetchInterval: refreshIntervals.live
  });

  const rows = usersQuery.data?.users ?? [];
  const total = usersQuery.data?.total ?? 0;

  function setSort(column: UserSort) {
    setFilters((current) =>
      current.sort === column
        ? { direction: current.direction === "asc" ? "desc" : "asc" }
        : { sort: column, direction: column === "traffic" ? "desc" : "asc" }
    );
  }

  // The hook never sees a row count, so the upper clamp lives here — in an
  // effect, because a setSearchParams during render is a navigation. It waits
  // for a response: while the first request is in flight `total` is 0, and
  // clamping on that would rewrite a deep-linked `?page=3` to page 1.
  const loaded = usersQuery.data !== undefined;
  const lastPage = Math.max(1, Math.ceil(total / perPage));
  useEffect(() => {
    if (loaded && page > lastPage) setPage(lastPage, "replace");
  }, [lastPage, loaded, page, setPage]);

  return (
    // `min-w-0`: this div is a grid item, and without it the table's min-width
    // becomes the page's min-width and the whole page scrolls sideways instead
    // of the table.
    <div className="flex min-h-full min-w-0 flex-col bg-kumo-canvas">
      <AppPageHeader
        title="Users"
        description="Manage proxy users, quotas, access counts, expiration, and traffic usage."
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
              <h2 className="text-base font-semibold text-kumo-default">User inventory</h2>
              <p className="text-sm text-kumo-subtle">
                {total > 0 ? `${total} ${total === 1 ? "user" : "users"}` : "No users"}
              </p>
            </div>

            <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
              <form
                className="flex min-w-0 flex-1 gap-2"
                onSubmit={form.handleSubmit((values) => setFilters({ search: values.search.trim() }))}
              >
                <Input
                  placeholder="Search by user, display name, or status"
                  aria-label="Search users"
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
                      onValueChange={(value) => setFilters({ status: value as UserFilterValues["status"] })}
                    >
                      {STATUS_FILTERS.map((value) => (
                        <DropdownMenu.RadioItem key={value} value={value}>
                          {FILTER_LABELS[value]}
                          <DropdownMenu.RadioItemIndicator />
                        </DropdownMenu.RadioItem>
                      ))}
                    </DropdownMenu.RadioGroup>
                  </DropdownMenu.Group>
                </DropdownMenu.Content>
              </DropdownMenu>
            </div>

            <TableCard>
              <Table layout="fixed" style={{ minWidth: tableMinWidth(userColumns) }}>
                <TableColgroup widths={userColumns} />
                <Table.Header variant="compact">
                  <Table.Row>
                    <SortHead label="User" column="name" sort={filters.sort} direction={filters.direction} setSort={setSort} sticky="left" />
                    <SortHead label="Status" column="status" sort={filters.sort} direction={filters.direction} setSort={setSort} />
                    <SortHead label="Traffic" column="traffic" sort={filters.sort} direction={filters.direction} setSort={setSort} />
                    <SortHead label="Quota" column="quota" sort={filters.sort} direction={filters.direction} setSort={setSort} />
                    <SortHead label="Access" column="proxy_count" sort={filters.sort} direction={filters.direction} setSort={setSort} />
                    <SortHead label="Expires" column="expire_at" sort={filters.sort} direction={filters.direction} setSort={setSort} />
                    <Table.Head className="text-right">
                      <span className="sr-only">Actions</span>
                    </Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {usersQuery.error ? (
                    <TableError colSpan={7}>{errorMessage(usersQuery.error)}</TableError>
                  ) : usersQuery.isLoading ? (
                    <TableLoading colSpan={7} />
                  ) : rows.length > 0 ? (
                    rows.map((row) => (
                      <Table.Row key={row.id}>
                        <Table.Cell sticky="left">
                          <div className="flex min-w-0 items-center gap-2">
                            <UserIcon className="size-4 shrink-0 text-kumo-subtle" />
                            <div className="min-w-0">
                              <div className="truncate text-base font-medium text-kumo-default" title={row.name}>
                                {row.name}
                              </div>
                              {row.display_name ? (
                                <div className="truncate text-sm text-kumo-subtle">{row.display_name}</div>
                              ) : null}
                            </div>
                          </div>
                        </Table.Cell>
                        <Table.Cell>
                          <StatusBadge tone={STATUS_TONES[row.effective_status]}>
                            {STATUS_LABELS[row.effective_status]}
                          </StatusBadge>
                        </Table.Cell>
                        <Table.Cell>
                          <div className="min-w-0">
                            <div className="truncate text-kumo-default">{formatBytes(billableBytes(row.traffic))}</div>
                            <div className="truncate text-xs text-kumo-subtle">raw {formatBytes(rawBytes(row.traffic))}</div>
                          </div>
                        </Table.Cell>
                        <Table.Cell>
                          <QuotaMeter quota={row.global_quota_bytes} traffic={row.traffic} />
                        </Table.Cell>
                        <Table.Cell>
                          <span className="block truncate text-kumo-subtle">{row.proxy_count}</span>
                        </Table.Cell>
                        <Table.Cell>
                          <span className="block truncate text-kumo-subtle" title={row.expire_at || undefined}>
                            {formatExpiry(row.expire_at)}
                          </span>
                        </Table.Cell>
                        <Table.Cell className="text-right">
                          <RowActionsMenu label={`Actions for ${row.name}`}>
                            {row.deleted_at ? (
                              <DropdownMenu.Item
                                icon={ArrowsClockwiseIcon}
                                disabled={restore.isPending}
                                onClick={() => restore.mutate(row)}
                              >
                                Restore
                              </DropdownMenu.Item>
                            ) : (
                              <>
                                <DropdownMenu.Item icon={PencilSimpleIcon} onClick={() => setDialog({ mode: "edit", user: row })}>
                                  Edit
                                </DropdownMenu.Item>
                                <DropdownMenu.Item icon={KeyIcon} onClick={() => setDialog({ mode: "access", user: row })}>
                                  Manage access
                                </DropdownMenu.Item>
                                <DropdownMenu.Item
                                  icon={IdentificationCardIcon}
                                  onClick={() => setDialog({ mode: "connection", user: row })}
                                >
                                  Connection info
                                </DropdownMenu.Item>
                                <DropdownMenu.Item
                                  icon={row.status === "disabled" ? CheckCircleIcon : ProhibitIcon}
                                  disabled={toggleStatus.isPending}
                                  onClick={() => toggleStatus.mutate(row)}
                                >
                                  {row.status === "disabled" ? "Enable" : "Disable"}
                                </DropdownMenu.Item>
                                <DropdownMenu.Separator />
                                <DropdownMenu.Item variant="danger" icon={TrashIcon} onClick={() => setDeleteTarget(row)}>
                                  Delete
                                </DropdownMenu.Item>
                              </>
                            )}
                          </RowActionsMenu>
                        </Table.Cell>
                      </Table.Row>
                    ))
                  ) : (
                    <TableEmpty colSpan={7}>No users match this filter.</TableEmpty>
                  )}
                </Table.Body>
              </Table>
            </TableCard>

            <AdminPagination page={page} setPage={setPage} perPage={perPage} setPerPage={setPerPage} total={total} />
          </section>
        </div>
      </main>

      {dialog?.mode === "create" || dialog?.mode === "edit" ? (
        <UserFormDialog request={request} state={dialog} onClose={() => setDialog(null)} />
      ) : null}
      {dialog?.mode === "access" ? (
        <ManageAccessDialog request={request} user={dialog.user} onClose={() => setDialog(null)} />
      ) : null}
      {dialog?.mode === "connection" ? (
        <ConnectionInfoDialog request={request} user={dialog.user} onClose={() => setDialog(null)} />
      ) : null}
      {deleteTarget ? (
        <SoftDeleteDialog
          request={request}
          title="Delete user"
          description={
            <>
              Delete <span className="font-medium text-kumo-default">{deleteTarget.name}</span>? The user and its credentials will disappear from the default inventory and can be restored from the Deleted filter.
            </>
          }
          endpoint={`/api/admin/users/${encodeURIComponent(deleteTarget.name)}`}
          onClose={() => setDeleteTarget(null)}
        />
      ) : null}
    </div>
  );
}
