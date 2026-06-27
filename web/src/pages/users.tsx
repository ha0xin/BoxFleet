import type { CSSProperties } from "react";
import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import type { Icon } from "@phosphor-icons/react";
import {
  CheckCircleIcon,
  DotsThreeIcon,
  FunnelIcon,
  KeyIcon,
  PencilSimpleIcon,
  PlusIcon,
  ProhibitIcon,
  SortAscendingIcon,
  SortDescendingIcon,
  UserIcon,
  WarningCircleIcon,
  XCircleIcon
} from "@phosphor-icons/react";
import { Button, DropdownMenu, Input, Loader, Meter, Pagination, Table } from "@cloudflare/kumo";

import type { AdminUser, TrafficRow } from "../types";
import { formatBytes } from "../utils";
import { PageHeader, PageTopBar } from "./operations-common";
import { useAdminMutation } from "@/admin/use-admin-mutation";
import { ManageAccessDialog, UserFormDialog } from "./user-dialogs";
import type { UserDialogState } from "./user-dialogs";

type AdminRequest = <T>(path: string, init?: RequestInit) => Promise<T>;
type UserFilter = "all" | "active" | "disabled" | "expired" | "quota_exceeded";
type UserSort = "name" | "status" | "traffic" | "quota" | "proxy_count" | "expire_at";
type SortDirection = "asc" | "desc";

type UserTraffic = {
  upload: number;
  download: number;
  rawUpload: number;
  rawDownload: number;
};

type UserRow = {
  user: AdminUser;
  traffic: UserTraffic;
  status: ReturnType<typeof userStatus>;
  total: number;
};

const emptyTraffic: UserTraffic = { upload: 0, download: 0, rawUpload: 0, rawDownload: 0 };

function trafficByUser(rows: TrafficRow[]): Map<string, UserTraffic> {
  const totals = new Map<string, UserTraffic>();
  for (const row of rows) {
    const current = totals.get(row.user_name) ?? { ...emptyTraffic };
    if (row.direction.includes("up")) {
      current.upload += row.billable_bytes;
      current.rawUpload += row.raw_bytes;
    } else {
      current.download += row.billable_bytes;
      current.rawDownload += row.raw_bytes;
    }
    totals.set(row.user_name, current);
  }
  return totals;
}

function isExpired(user: AdminUser): boolean {
  if (!user.expire_at) return false;
  const time = new Date(user.expire_at).getTime();
  return Number.isFinite(time) && time <= Date.now();
}

function formatExpiry(value: string): string {
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

function userStatus(user: AdminUser, total: number): {
  key: Exclude<UserFilter, "all">;
  label: string;
  icon: Icon;
  className: string;
} {
  if (user.status === "disabled") {
    return { key: "disabled", label: "Disabled", icon: XCircleIcon, className: "text-kumo-subtle" };
  }
  if (user.status === "quota_exceeded" || (user.global_quota_bytes > 0 && total >= user.global_quota_bytes)) {
    return { key: "quota_exceeded", label: "Over quota", icon: WarningCircleIcon, className: "text-kumo-warning" };
  }
  if (user.status === "expired" || isExpired(user)) {
    return { key: "expired", label: "Expired", icon: WarningCircleIcon, className: "text-kumo-warning" };
  }
  return { key: "active", label: "Active", icon: CheckCircleIcon, className: "text-kumo-success" };
}

function compareText(left: string | number | undefined, right: string | number | undefined, direction: SortDirection) {
  return String(left ?? "").localeCompare(String(right ?? ""), undefined, { numeric: true }) * (direction === "desc" ? -1 : 1);
}

function formatSupplementaryError(error: unknown): string {
  if (!(error instanceof Error)) return "Traffic request failed.";
  const match = error.message.match(/^(\d{3})\s+(.+)$/);
  if (!match) return error.message;
  const [, status, body] = match;
  try {
    const parsed = JSON.parse(body) as { error?: string };
    return `Traffic request failed (${status}): ${parsed.error || body}`;
  } catch {
    return `Traffic request failed (${status}): ${body}`;
  }
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
  column: UserSort;
  sort: UserSort;
  direction: SortDirection;
  setSort: (column: UserSort) => void;
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
      <Table.Cell colSpan={7}>
        <div className="flex min-h-32 items-center justify-center text-sm text-kumo-subtle">{children}</div>
      </Table.Cell>
    </Table.Row>
  );
}

function QuotaMeter({ user, traffic }: { user: AdminUser; traffic: UserTraffic }) {
  const total = traffic.upload + traffic.download;
  const quota = user.global_quota_bytes;
  const max = quota > 0 ? quota : Math.max(total, 1);
  const value = quota > 0 ? Math.min(total, quota) : total;
  const uploadShare = total > 0 ? Math.round((traffic.upload / total) * 100) : 0;
  const style = {
    "--meter-split": `${uploadShare}%`
  } as CSSProperties;

  return (
    <div className="min-w-64">
      <Meter
        label={quota > 0 ? "Quota" : "Traffic"}
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
          {formatBytes(traffic.upload)} up
        </span>
        <span className="inline-flex items-center gap-1">
          <span className="size-2 rounded-full bg-kumo-success" />
          {formatBytes(traffic.download)} down
        </span>
      </div>
    </div>
  );
}

function sortRows(rows: UserRow[], sort: UserSort, direction: SortDirection): UserRow[] {
  return [...rows].sort((a, b) => {
    switch (sort) {
      case "status":
        return compareText(a.status.label, b.status.label, direction) || compareText(a.user.name, b.user.name, "asc");
      case "traffic":
        return compareText(a.total, b.total, direction) || compareText(a.user.name, b.user.name, "asc");
      case "quota":
        return compareText(a.user.global_quota_bytes, b.user.global_quota_bytes, direction) || compareText(a.user.name, b.user.name, "asc");
      case "proxy_count":
        return compareText(a.user.proxy_count, b.user.proxy_count, direction) || compareText(a.user.name, b.user.name, "asc");
      case "expire_at":
        return compareText(a.user.expire_at, b.user.expire_at, direction) || compareText(a.user.name, b.user.name, "asc");
      default:
        return compareText(a.user.name, b.user.name, direction);
    }
  });
}

export function UsersPage({ request }: { request: AdminRequest }) {
  const [page, setPage] = useState(1);
  const [perPage, setPerPage] = useState(10);
  const [filter, setFilter] = useState<UserFilter>("all");
  const [searchInput, setSearchInput] = useState("");
  const [search, setSearch] = useState("");
  const [sort, setSortValue] = useState<UserSort>("name");
  const [direction, setDirection] = useState<SortDirection>("asc");
  const [dialog, setDialog] = useState<UserDialogState>(null);

  const toggleStatus = useAdminMutation<AdminUser>(request, (req, user) =>
    req(`/api/admin/users/${encodeURIComponent(user.name)}`, {
      method: "PATCH",
      body: JSON.stringify({ status: user.status === "disabled" ? "active" : "disabled" })
    })
  );

  const usersQuery = useQuery({
    queryKey: ["admin", "users"],
    queryFn: () => request<AdminUser[]>("/api/admin/users")
  });
  const trafficQuery = useQuery({
    queryKey: ["admin", "traffic-users"],
    queryFn: () => request<TrafficRow[]>("/api/admin/traffic/users")
  });

  function setSort(column: UserSort) {
    setPage(1);
    if (sort === column) {
      setDirection((value) => (value === "asc" ? "desc" : "asc"));
      return;
    }
    setSortValue(column);
    setDirection(column === "traffic" ? "desc" : "asc");
  }

  function setFilterValue(value: UserFilter) {
    setFilter(value);
    setPage(1);
  }

  function setPageSize(value: number) {
    setPerPage(value);
    setPage(1);
  }

  const rows = useMemo(() => {
    const totals = trafficByUser(trafficQuery.data ?? []);
    return (usersQuery.data ?? []).map((user) => {
      const traffic = totals.get(user.name) ?? emptyTraffic;
      const total = traffic.upload + traffic.download;
      return { user, traffic, total, status: userStatus(user, total) };
    });
  }, [trafficQuery.data, usersQuery.data]);

  const filtered = useMemo(() => {
    const needle = search.trim().toLowerCase();
    return sortRows(
      rows.filter((row) => {
        if (filter !== "all" && row.status.key !== filter) return false;
        if (!needle) return true;
        return [row.user.name, row.user.display_name, row.user.status, row.status.label]
          .some((value) => value.toLowerCase().includes(needle));
      }),
      sort,
      direction
    );
  }, [direction, filter, rows, search, sort]);

  const offset = (page - 1) * perPage;
  const visibleRows = filtered.slice(offset, offset + perPage);
  const loading = usersQuery.isLoading;
  const error = usersQuery.error;
  const trafficError = trafficQuery.error ? formatSupplementaryError(trafficQuery.error) : "";
  const total = filtered.length;

  return (
    <div className="flex min-h-full flex-col bg-kumo-canvas">
      <PageTopBar current="Users" />
      <div className="relative z-[19] min-h-21 bg-kumo-canvas pb-2">
        <div className="mx-auto w-full max-w-[1400px] px-6 pt-3 pb-1 md:px-8 lg:px-10" />
      </div>
      <main className="w-full grow bg-kumo-canvas">
        <PageHeader
          title="Users"
          description="Manage proxy users, quotas, access counts, expiration, and traffic usage."
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
                <h2 className="text-base font-semibold text-kumo-default">User inventory</h2>
                <p className="text-sm text-kumo-subtle">
                  {total > 0 ? `Showing ${offset + 1}-${Math.min(offset + perPage, total)} of ${total}` : "No users"}
                </p>
              </div>
              {trafficError ? (
                <p className="max-w-xl text-sm text-kumo-warning">
                  Traffic usage is unavailable: {trafficError}
                </p>
              ) : null}
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
                  placeholder="Search by user, display name, or status"
                  aria-label="Search users"
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
                      onValueChange={(value) => setFilterValue(value as UserFilter)}
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
                      <DropdownMenu.RadioItem value="expired">
                        Expired
                        <DropdownMenu.RadioItemIndicator />
                      </DropdownMenu.RadioItem>
                      <DropdownMenu.RadioItem value="quota_exceeded">
                        Over quota
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
                      <SortHead label="User" column="name" sort={sort} direction={direction} setSort={setSort} />
                      <SortHead label="Status" column="status" sort={sort} direction={direction} setSort={setSort} />
                      <SortHead label="Traffic" column="traffic" sort={sort} direction={direction} setSort={setSort} />
                      <SortHead label="Quota" column="quota" sort={sort} direction={direction} setSort={setSort} />
                      <SortHead label="Access" column="proxy_count" sort={sort} direction={direction} setSort={setSort} />
                      <SortHead label="Expires" column="expire_at" sort={sort} direction={direction} setSort={setSort} />
                      <Table.Head className="text-right">
                        <span className="sr-only">Actions</span>
                      </Table.Head>
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {error ? (
                      <TableEmpty>{error instanceof Error ? error.message : "Request failed."}</TableEmpty>
                    ) : loading ? (
                      <Table.Row>
                        <Table.Cell colSpan={7}>
                          <div className="flex min-h-32 items-center justify-center">
                            <Loader size={20} />
                          </div>
                        </Table.Cell>
                      </Table.Row>
                    ) : visibleRows.length > 0 ? (
                      visibleRows.map((row) => {
                        const StatusIcon = row.status.icon;
                        return (
                          <Table.Row key={row.user.id}>
                            <Table.Cell>
                              <div className="flex min-w-52 items-center gap-2">
                                <UserIcon className="size-4 shrink-0 text-kumo-subtle" />
                                <div className="min-w-0">
                                  <div className="truncate text-base font-medium text-kumo-default">{row.user.name}</div>
                                  {row.user.display_name ? (
                                    <div className="truncate text-sm text-kumo-subtle">{row.user.display_name}</div>
                                  ) : null}
                                </div>
                              </div>
                            </Table.Cell>
                            <Table.Cell>
                              <span className={`inline-flex items-center gap-1.5 whitespace-nowrap text-sm font-medium ${row.status.className}`}>
                                <StatusIcon className="size-4 shrink-0" />
                                {row.status.label}
                              </span>
                            </Table.Cell>
                            <Table.Cell>
                              <div className="whitespace-nowrap">
                                <div className="text-kumo-default">{formatBytes(row.total)}</div>
                                <div className="text-xs text-kumo-subtle">
                                  raw {formatBytes(row.traffic.rawUpload + row.traffic.rawDownload)}
                                </div>
                              </div>
                            </Table.Cell>
                            <Table.Cell>
                              <QuotaMeter user={row.user} traffic={row.traffic} />
                            </Table.Cell>
                            <Table.Cell>
                              <span className="whitespace-nowrap text-kumo-subtle">{row.user.proxy_count}</span>
                            </Table.Cell>
                            <Table.Cell>
                              <span className="whitespace-nowrap text-kumo-subtle">
                                {formatExpiry(row.user.expire_at)}
                              </span>
                            </Table.Cell>
                            <Table.Cell className="text-right">
                              <DropdownMenu>
                                <DropdownMenu.Trigger
                                  render={
                                    <Button variant="ghost" size="sm" shape="square" aria-label={`Actions for ${row.user.name}`}>
                                      <DotsThreeIcon className="size-4" />
                                    </Button>
                                  }
                                />
                                <DropdownMenu.Content>
                                  <DropdownMenu.Item icon={PencilSimpleIcon} onClick={() => setDialog({ mode: "edit", user: row.user })}>
                                    Edit
                                  </DropdownMenu.Item>
                                  <DropdownMenu.Item icon={KeyIcon} onClick={() => setDialog({ mode: "access", user: row.user })}>
                                    Manage access
                                  </DropdownMenu.Item>
                                  <DropdownMenu.Item
                                    icon={row.user.status === "disabled" ? CheckCircleIcon : ProhibitIcon}
                                    onClick={() => toggleStatus.mutate(row.user)}
                                  >
                                    {row.user.status === "disabled" ? "Enable" : "Disable"}
                                  </DropdownMenu.Item>
                                </DropdownMenu.Content>
                              </DropdownMenu>
                            </Table.Cell>
                          </Table.Row>
                        );
                      })
                    ) : (
                      <TableEmpty>No users match this filter.</TableEmpty>
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

      {dialog?.mode === "create" || dialog?.mode === "edit" ? (
        <UserFormDialog request={request} state={dialog} onClose={() => setDialog(null)} />
      ) : null}
      {dialog?.mode === "access" ? (
        <ManageAccessDialog request={request} user={dialog.user} onClose={() => setDialog(null)} />
      ) : null}
    </div>
  );
}
