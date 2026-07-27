import { useEffect, useMemo, useState } from "react";
import type { MouseEvent, ReactNode } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { startOfDay, startOfHour, subDays } from "date-fns";
import {
  ArrowRightIcon,
  ArrowsClockwiseIcon,
  ChartLineUpIcon,
  GaugeIcon,
  HardDrivesIcon,
  ListChecksIcon,
  PlusIcon,
  ShieldCheckIcon,
  UsersIcon
} from "@phosphor-icons/react";
import type { Icon } from "@phosphor-icons/react";
import { Button, Empty, LayerCard, Link } from "@cloudflare/kumo";

import { useAdminApi } from "@/admin/api";
import { adminKeys, queryString, refreshIntervals } from "@/admin/query";
import { AppPageHeader } from "@/components/app-page-header";
import { Sparkline } from "@/components/sparkline";
import { StatusBadge } from "@/components/status-badge";
import type {
  AdminNode,
  AdminUser,
  NetworkEventSeriesResponse,
  Overview,
  SystemLog,
  TrafficRow,
  TrafficSeriesResponse
} from "../types";
import { formatBytes } from "../utils";
import {
  adminPath,
  formatCompactNumber,
  formatNodeVersion,
  isNodeDrifting,
  isNodeOnline,
  nodeHealth,
  rowDelay,
  rowLinkClassName,
  toneClass,
  WidgetHeader,
  type Tone
} from "./operations-common";

type TrafficByUser = { user: string; upload: number; download: number };

// Overview trends are a fixed seven-day, day-bucketed window. The server owns
// bucketing and zero-fill, so every series arrives contiguous and oldest first.
const TREND_DAYS = 7;
const TREND_LABEL = "last 7 days";
// Node rows only show the first few nodes, but the ranking is by volume, so ask
// for the full page rather than the default 25 to keep quiet nodes covered.
const NODE_TREND_LIMIT = 100;
const TREND_TONE = "text-kumo-info";

const USER_STATUS_LABELS: Record<string, string> = {
  active: "Active",
  disabled: "Disabled"
};

function userStatusLabel(status: string): string {
  if (USER_STATUS_LABELS[status]) return USER_STATUS_LABELS[status];
  return status ? status.charAt(0).toUpperCase() + status.slice(1) : "Unknown";
}

// The server returns at most 10 recent system logs, so counts saturate there.
function formatLogCount(count: number): string {
  return count >= 10 ? "10+" : formatCompactNumber(count);
}

function useSpaNavigate() {
  const navigate = useNavigate();
  return (path: string) => (event: MouseEvent<HTMLElement>) => {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    navigate(path);
  };
}

function groupTrafficByUser(rows: TrafficRow[]): TrafficByUser[] {
  const byUser = new Map<string, TrafficByUser>();
  for (const row of rows) {
    const entry = byUser.get(row.user_name) ?? { user: row.user_name, upload: 0, download: 0 };
    if (row.direction.includes("up")) {
      entry.upload += row.billable_bytes;
    } else {
      entry.download += row.billable_bytes;
    }
    byUser.set(row.user_name, entry);
  }
  return [...byUser.values()].sort((a, b) => b.upload + b.download - (a.upload + a.download));
}

type TrendWindow = { start: string; end: string; bucket: "day"; offset_minutes: number };

function useTrendWindow(): TrendWindow {
  // Truncating the window end to the hour keeps the query key — and with it the
  // cache entry — stable while the operator moves between pages. The anchor
  // advances when the wall clock crosses into the next hour so a long-lived tab
  // keeps pulling the current day's rows into the window.
  const [anchor, setAnchor] = useState(() => startOfHour(new Date()));
  useEffect(() => {
    const untilNextHour = anchor.getTime() + 3_600_000 - Date.now();
    const timer = setTimeout(
      () => setAnchor(startOfHour(new Date())),
      Math.max(untilNextHour, 1_000)
    );
    return () => clearTimeout(timer);
  }, [anchor]);
  return useMemo(
    () => ({
      start: startOfDay(subDays(anchor, TREND_DAYS - 1)).toISOString(),
      end: anchor.toISOString(),
      bucket: "day",
      // The server adds this to UTC to get local time, the opposite of the JS sign.
      offset_minutes: -anchor.getTimezoneOffset()
    }),
    [anchor]
  );
}

function useTrafficTrend(trend: TrendWindow, group: "total" | "node") {
  const { request } = useAdminApi();
  const params = { ...trend, group, limit: group === "node" ? NODE_TREND_LIMIT : undefined };
  return useQuery({
    queryKey: adminKeys.trafficSeries(params),
    queryFn: ({ signal }) =>
      request<TrafficSeriesResponse>(`/api/admin/traffic/series${queryString(params)}`, { signal }),
    staleTime: 60_000,
    // Late-arriving node reports can still merge into buckets inside the window.
    refetchInterval: refreshIntervals.slow
  });
}

function useNetworkEventTrend(trend: TrendWindow) {
  const { request } = useAdminApi();
  const params = { ...trend, group: "total" };
  return useQuery({
    queryKey: adminKeys.networkEventSeries(params),
    queryFn: ({ signal }) =>
      request<NetworkEventSeriesResponse>(`/api/admin/network-events/series${queryString(params)}`, { signal }),
    staleTime: 60_000,
    refetchInterval: refreshIntervals.slow
  });
}

function bucketBytes(point: { uplink_billable_bytes: number; downlink_billable_bytes: number }): number {
  return point.uplink_billable_bytes + point.downlink_billable_bytes;
}

export function trafficTrendValues(response: TrafficSeriesResponse | undefined, key = "total"): number[] {
  const series = response?.series.find((entry) => entry.key === key);
  return series ? series.points.map(bucketBytes) : [];
}

export function trafficTrendTotal(response: TrafficSeriesResponse | undefined, key = "total"): number | null {
  const series = response?.series.find((entry) => entry.key === key);
  return series ? bucketBytes(series.totals) : null;
}

export function nodeTrendValues(response: TrafficSeriesResponse | undefined): Map<string, number[]> {
  const trends = new Map<string, number[]>();
  for (const series of response?.series ?? []) {
    trends.set(series.key, series.points.map(bucketBytes));
  }
  return trends;
}

export function networkEventTrend(response: NetworkEventSeriesResponse | undefined): {
  values: number[];
  total: number | null;
} {
  const series = response?.series.find((entry) => entry.key === "total");
  if (!series) return { values: [], total: null };
  return { values: series.points.map((point) => point.count), total: series.total };
}

function AnalyticsTile({
  label,
  value,
  detail,
  href,
  trend,
  trendLabel,
  delay = 0
}: {
  label: string;
  value: string;
  detail?: string;
  href: string;
  trend?: readonly number[];
  trendLabel?: string;
  delay?: number;
}) {
  const spaNavigate = useSpaNavigate();
  const sparkline = trend && trend.length > 1 && trendLabel ? trend : null;
  return (
    <Link
      href={adminPath(href)}
      onClick={spaNavigate(href)}
      variant="current"
      className="group flex h-full w-full !no-underline outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-kumo-brand animate-fade-slide-in"
      style={rowDelay(delay)}
    >
      <div
        className={`flex h-full min-h-22 w-full flex-col gap-2 overflow-hidden bg-kumo-base px-4 pt-4 transition-colors group-hover:bg-kumo-tint group-focus-visible:bg-kumo-tint ${
          sparkline ? "pb-0" : "pb-4"
        }`}
      >
        <div className="flex items-center text-xs font-medium text-kumo-subtle">
          {label}
        </div>
        <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
          <span className="text-xl font-semibold leading-none text-kumo-default">{value}</span>
          {detail ? <span className="text-sm font-medium text-kumo-subtle">{detail}</span> : null}
        </div>
        {sparkline && trendLabel ? (
          <div className={`-mx-4 mt-auto h-8 w-[calc(100%+2rem)] min-w-0 ${TREND_TONE}`}>
            <Sparkline values={sparkline} label={trendLabel} />
          </div>
        ) : null}
      </div>
    </Link>
  );
}

function AnalyticsCard({
  title,
  icon,
  tiles
}: {
  title: string;
  icon: Icon;
  tiles: Array<Parameters<typeof AnalyticsTile>[0]>;
}) {
  return (
    <LayerCard className="flex h-full w-full flex-col">
      <WidgetHeader title={title} icon={icon} />
      <LayerCard.Primary className="flex-1 p-0">
        <div className="grid h-full auto-rows-fr grid-cols-1 sm:grid-cols-2">
          {tiles.map((tile, index) => (
            <div
              key={tile.label}
              className={index === 0 ? "border-b border-kumo-line sm:border-r sm:border-b-0" : ""}
            >
              <AnalyticsTile {...tile} delay={index} />
            </div>
          ))}
        </div>
      </LayerCard.Primary>
    </LayerCard>
  );
}

type ListWidgetItem = {
  label: string;
  // Router path ("/users"); rendered with the /admin prefix so middle-click works.
  href: string;
  value?: string;
  valueBadge?: ReactNode;
  detail?: string;
  icon?: Icon;
  iconClassName?: string;
  valueTone?: Tone;
  external?: boolean;
};

function SimpleListWidget({
  title,
  count,
  icon,
  href,
  actionHref,
  emptyTitle,
  items
}: {
  title: string;
  count?: number;
  icon?: Icon;
  href?: string;
  actionHref?: string;
  emptyTitle?: string;
  items: ListWidgetItem[];
}) {
  const spaNavigate = useSpaNavigate();
  return (
    <LayerCard className="flex h-full w-full flex-col">
      <WidgetHeader title={title} count={count} icon={icon} href={href} actionHref={actionHref} />
      <LayerCard.Primary className="flex-1 p-0">
        <div className="relative flex-1">
          {items.length === 0 ? (
            <div className="flex min-h-36 items-center justify-center p-4">
              <Empty size="sm" title={emptyTitle ?? "No items"} />
            </div>
          ) : (
            <ul role="list" className="mx-3 flex flex-col divide-y divide-kumo-hairline">
              {items.map((item, index) => {
                const ItemIcon = item.icon;
                return (
                  <li
                    key={`${item.label}-${item.href}-${index}`}
                    className="flex h-12 items-center justify-between gap-3 px-1 animate-fade-slide-in"
                    style={rowDelay(index)}
                  >
                    <div className="flex min-w-0 flex-1 items-center gap-2">
                      {ItemIcon ? (
                        <ItemIcon className={`size-4 shrink-0 ${item.iconClassName ?? "text-kumo-subtle"}`} />
                      ) : null}
                      <div className="min-w-0 flex-1 overflow-hidden">
                        <Link
                          href={item.external ? item.href : adminPath(item.href)}
                          onClick={item.external ? undefined : spaNavigate(item.href)}
                          variant="current"
                          target={item.external ? "_blank" : undefined}
                          rel={item.external ? "noreferrer" : undefined}
                          className={`${rowLinkClassName} flex w-full items-center gap-2 overflow-hidden`}
                        >
                          <span className="truncate">{item.label}</span>
                          {item.external ? <ArrowRightIcon className="size-4 shrink-0 text-kumo-subtle" /> : null}
                        </Link>
                        {item.detail ? <div className="truncate text-xs text-kumo-subtle">{item.detail}</div> : null}
                      </div>
                    </div>
                    {item.valueBadge ? (
                      <span className="shrink-0">{item.valueBadge}</span>
                    ) : item.value ? (
                      <span className={`max-w-[40%] shrink-0 truncate whitespace-nowrap text-sm font-medium tabular-nums ${toneClass(item.valueTone ?? "subtle")}`} title={item.value}>
                        {item.value}
                      </span>
                    ) : null}
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      </LayerCard.Primary>
    </LayerCard>
  );
}

function NodesWidget({ nodes, trends }: { nodes: AdminNode[]; trends: Map<string, number[]> }) {
  const spaNavigate = useSpaNavigate();
  const rows = nodes.slice(0, 4);
  return (
    <LayerCard className="flex h-full w-full flex-col">
      <WidgetHeader title="Nodes" count={nodes.length} href="/nodes" actionHref="/nodes" />
      <LayerCard.Primary className="flex-1 p-0">
        <div className="relative flex-1">
          {rows.length > 0 ? (
            <ul role="list" className="mx-3 flex flex-col divide-y divide-kumo-hairline">
              {rows.map((node, index) => {
                const status = nodeHealth(node);
                const StatusIcon = status.icon;
                const trend = trends.get(node.name) ?? [];
                return (
                  <li
                    key={node.id}
                    className="group/row grid h-12 grid-cols-[auto_minmax(0,1fr)_7rem] items-center gap-2 px-1 animate-fade-slide-in"
                    style={rowDelay(index)}
                  >
                    <StatusIcon className={`size-5 shrink-0 ${status.className}`} aria-label={status.label} />
                    <div className="relative ml-2 grid min-w-0 grid-cols-[1fr_40%] items-center self-stretch overflow-hidden">
                      <div className="z-10 col-span-2 col-start-1 row-start-1 flex min-w-0 items-center pr-10">
                        <Link
                          href={adminPath("/nodes")}
                          onClick={spaNavigate("/nodes")}
                          variant="current"
                          className={`${rowLinkClassName} block max-w-full truncate bg-kumo-base pr-2`}
                          title={node.name}
                        >
                          {node.name}
                        </Link>
                      </div>
                      {trend.length > 1 ? (
                        <div className={`absolute right-0 bottom-0 h-8 w-[40%] min-w-0 pb-px ${TREND_TONE}`}>
                          <Sparkline values={trend} label={`Billable traffic for ${node.name}, ${TREND_LABEL}`} />
                        </div>
                      ) : null}
                    </div>
                    <span className="truncate text-right text-sm text-kumo-subtle" title={formatNodeVersion(node)}>{formatNodeVersion(node)}</span>
                  </li>
                );
              })}
            </ul>
          ) : (
            <div className="flex min-h-36 items-center justify-center p-4">
              <Empty size="sm" title="No nodes" />
            </div>
          )}
        </div>
      </LayerCard.Primary>
    </LayerCard>
  );
}

function buildUserItems(users: AdminUser[]): ListWidgetItem[] {
  return users.slice(0, 4).map((user) => ({
    label: user.display_name || user.name,
    href: "/users",
    valueBadge: (
      <StatusBadge tone={user.status === "active" ? "success" : "neutral"}>
        {userStatusLabel(user.status)}
      </StatusBadge>
    ),
    detail: `${user.proxy_count} proxies`,
    icon: UsersIcon
  }));
}

function buildTrafficItems(trafficUsers: TrafficByUser[]): ListWidgetItem[] {
  return trafficUsers.slice(0, 4).map((row) => ({
    label: row.user,
    href: "/traffic",
    value: formatBytes(row.upload + row.download),
    detail: `${formatBytes(row.upload)} up, ${formatBytes(row.download)} down`,
    icon: GaugeIcon
  }));
}

function buildSystemItems(overview: Overview | null): ListWidgetItem[] {
  const logs = overview?.system_logs ?? [];
  const release = overview?.release;
  const note = overview?.system_log_note;
  const items: ListWidgetItem[] = [
    {
      label: "BoxFleet server",
      href: "/settings",
      value: release?.boxfleet_version ?? "—",
      icon: HardDrivesIcon
    },
    {
      label: "sing-box target",
      href: "/settings",
      value: release?.sing_box_version ?? "—",
      icon: ArrowsClockwiseIcon
    },
    {
      label: "System logs",
      href: "/system-logs",
      value: formatLogCount(logs.length),
      detail: note || undefined,
      icon: ListChecksIcon
    }
  ];
  return items;
}

function latestLogTone(log: SystemLog): Tone {
  const level = (log.level || "").toLowerCase();
  if (level.includes("error") || level.includes("fatal")) return "danger";
  if (level.includes("warn")) return "warning";
  return "subtle";
}

function buildLogItems(logs: SystemLog[]): ListWidgetItem[] {
  return logs.slice(0, 4).map((log) => ({
    label: log.message,
    href: "/system-logs",
    value: log.level || "—",
    detail: `${log.node} · ${log.service}`,
    icon: ListChecksIcon,
    valueTone: latestLogTone(log)
  }));
}

function NextStepsWidget() {
  return (
    <SimpleListWidget
      title="Next steps"
      items={[
        {
          label: "Create or enroll a node",
          href: "/nodes",
          icon: HardDrivesIcon
        },
        {
          label: "Invite users and issue access",
          href: "/users",
          icon: UsersIcon
        },
        {
          label: "Review recent network events",
          href: "/network-events",
          icon: ChartLineUpIcon
        }
      ]}
    />
  );
}

function OverviewGridItem({ children, wide = false }: { children: ReactNode; wide?: boolean }) {
  return (
    <div className={wide ? "col-span-6 md:col-span-6 xl:col-span-4" : "col-span-6 md:col-span-3 xl:col-span-2"}>
      {children}
    </div>
  );
}

export function OverviewPage({ overview }: { overview: Overview | null }) {
  const navigate = useNavigate();
  const nodes = overview?.nodes ?? [];
  const users = overview?.users ?? [];
  const trafficRows = overview?.traffic ?? [];
  const logs = overview?.system_logs ?? [];

  const activeNodes = nodes.filter(isNodeOnline).length;
  const activeUsers = users.filter((u) => u.status === "active").length;
  const driftingNodes = nodes.filter(isNodeDrifting).length;
  const attentionNodes = nodes.filter((node) => nodeHealth(node).label !== "Online").length;
  const totalTraffic = trafficRows.reduce((sum, row) => sum + row.billable_bytes, 0);
  const trafficUsers = groupTrafficByUser(trafficRows);

  const trendWindow = useTrendWindow();
  const trafficTrendQuery = useTrafficTrend(trendWindow, "total");
  const nodeTrafficTrendQuery = useTrafficTrend(trendWindow, "node");
  const eventTrendQuery = useNetworkEventTrend(trendWindow);

  const trafficTrend = trafficTrendValues(trafficTrendQuery.data);
  const trafficTrendWindowTotal = trafficTrendTotal(trafficTrendQuery.data);
  const nodeTrends = useMemo(() => nodeTrendValues(nodeTrafficTrendQuery.data), [nodeTrafficTrendQuery.data]);
  const eventTrend = networkEventTrend(eventTrendQuery.data);

  return (
    <div className="flex min-h-full flex-col bg-kumo-canvas">
      <AppPageHeader
        title="Overview"
        description="Central control plane for nodes, users, traffic, and config versions."
        actions={
          <Button variant="primary" icon={PlusIcon} onClick={() => navigate("/nodes")}>
            Add node
          </Button>
        }
      />
      <main className="w-full grow bg-kumo-canvas">
        <div className="mx-auto flex w-full max-w-[1400px] flex-col gap-4 px-6 pb-8 md:px-8 lg:px-10">
          <div className="grid auto-rows-min grid-cols-6 gap-4 tabular-nums">
            <div className="col-span-6">
              <section aria-label="Analytics" className="w-full space-y-3">
                <div>
                  <h2 className="text-base font-semibold text-kumo-default">Analytics</h2>
                  <p className="text-xs text-kumo-subtle">Trend lines cover the {TREND_LABEL}, one point per day.</p>
                </div>
                <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
                  <AnalyticsCard
                    title="Security"
                    icon={ShieldCheckIcon}
                    tiles={[
                      {
                        label: "Nodes needing attention",
                        value: formatCompactNumber(attentionNodes),
                        detail: `${driftingNodes} config drift`,
                        href: "/nodes"
                      },
                      {
                        label: "System warnings",
                        value: formatCompactNumber(logs.filter((log) => latestLogTone(log) !== "subtle").length),
                        detail: `${formatLogCount(logs.length)} log events`,
                        href: "/system-logs"
                      }
                    ]}
                  />
                  <AnalyticsCard
                    title="Performance"
                    icon={GaugeIcon}
                    tiles={[
                      {
                        label: "Billable traffic",
                        value: formatBytes(totalTraffic),
                        detail: trafficTrendWindowTotal === null
                          ? undefined
                          : `${formatBytes(trafficTrendWindowTotal)} ${TREND_LABEL}`,
                        href: "/traffic",
                        trend: trafficTrend,
                        trendLabel: `Billable traffic, ${TREND_LABEL}`
                      },
                      {
                        label: "Traffic users",
                        value: formatCompactNumber(trafficUsers.length),
                        detail: `${activeUsers}/${users.length} active`,
                        href: "/traffic"
                      }
                    ]}
                  />
                  <AnalyticsCard
                    title="Activity"
                    icon={ChartLineUpIcon}
                    tiles={[
                      {
                        // No historical node-status table exists, so this tile
                        // carries no trend line rather than an invented one.
                        label: "Active nodes",
                        value: `${activeNodes}/${nodes.length}`,
                        href: "/nodes"
                      },
                      {
                        label: "Network events",
                        value: eventTrend.total === null ? "—" : formatCompactNumber(eventTrend.total),
                        detail: TREND_LABEL,
                        href: "/network-events",
                        trend: eventTrend.values,
                        trendLabel: `Network events, ${TREND_LABEL}`
                      }
                    ]}
                  />
                </div>
              </section>
            </div>

            <OverviewGridItem>
              <NodesWidget nodes={nodes} trends={nodeTrends} />
            </OverviewGridItem>
            <OverviewGridItem>
              <SimpleListWidget
                title="Users"
                count={users.length}
                icon={UsersIcon}
                href="/users"
                actionHref="/users"
                emptyTitle="No users"
                items={buildUserItems(users)}
              />
            </OverviewGridItem>
            <OverviewGridItem>
              <SimpleListWidget
                title="Traffic"
                count={trafficUsers.length}
                icon={GaugeIcon}
                href="/traffic"
                emptyTitle="No traffic yet"
                items={buildTrafficItems(trafficUsers)}
              />
            </OverviewGridItem>
            <OverviewGridItem>
              <SimpleListWidget
                title="System"
                icon={HardDrivesIcon}
                href="/settings"
                items={buildSystemItems(overview)}
              />
            </OverviewGridItem>
            <OverviewGridItem wide>
              <SimpleListWidget
                title="System logs"
                count={logs.length}
                icon={ListChecksIcon}
                href="/system-logs"
                emptyTitle="No recent log events"
                items={buildLogItems(logs)}
              />
            </OverviewGridItem>
            <OverviewGridItem>
              <NextStepsWidget />
            </OverviewGridItem>
          </div>
        </div>
      </main>
    </div>
  );
}
