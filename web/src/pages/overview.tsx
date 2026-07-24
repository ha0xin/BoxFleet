import type { ReactNode } from "react";
import {
  ArrowRightIcon,
  ArrowsClockwiseIcon,
  CalendarBlankIcon,
  ChartLineUpIcon,
  CheckCircleIcon,
  GaugeIcon,
  HardDrivesIcon,
  ListChecksIcon,
  PlusIcon,
  ShieldCheckIcon,
  TrendUpIcon,
  UsersIcon,
  XCircleIcon
} from "@phosphor-icons/react";
import type { Icon } from "@phosphor-icons/react";
import { Button, LayerCard, Link, LinkButton } from "@cloudflare/kumo";

import type { AdminNode, AdminUser, Overview, SystemLog, TrafficRow } from "../types";
import { formatBytes } from "../utils";
import {
  adminPath,
  formatCompactNumber,
  formatNodeVersion,
  isNodeDrifting,
  isNodeOnline,
  nodeHealth,
  PageHeader,
  PageTopBar,
  rowDelay,
  SparkArea,
  toSparkline,
  toneClass,
  WidgetHeader,
  type SparklinePoint,
  type Tone
} from "./operations-common";

type TrafficByUser = { user: string; upload: number; download: number };

// Placeholder shapes preserve the dashboard layout until the server exposes
// time-series telemetry. They must not be interpreted as measurements. When the
// time-series API lands, enable the time-range control and the tile's "live" delta.
const PLACEHOLDER_NODE_SPARKLINES = [
  [18, 17, 19, 18, 21, 20, 22, 23, 21, 24, 26, 25, 27, 29, 31, 28, 30, 33],
  [8, 8, 9, 40, 10, 9, 11, 33, 10, 9, 10, 9, 11, 10, 12, 16, 10, 9],
  [12, 14, 11, 26, 13, 12, 15, 32, 14, 23, 13, 12, 11, 26, 12, 13, 11, 12],
  [10, 10, 10, 11, 10, 10, 12, 10, 10, 11, 10, 10, 10, 11, 10, 10, 10, 10],
] as const;

const PLACEHOLDER_ANALYTICS_SPARKLINES = {
  traffic: [0, 12, 8, 18, 16, 22, 20, 26, 24, 29, 28, 36, 34, 41, 39, 44, 42, 48],
  users: [8, 9, 10, 10, 12, 12, 13, 15, 15, 16, 18, 19, 18, 21, 23, 22, 24, 26],
  logs: [4, 8, 5, 12, 7, 10, 13, 9, 14, 18, 16, 20, 17, 22, 24, 21, 23, 25],
} as const;

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

function placeholderNodeSparkline(index: number): SparklinePoint[] {
  return toSparkline(PLACEHOLDER_NODE_SPARKLINES[index % PLACEHOLDER_NODE_SPARKLINES.length]);
}

function AnalyticsTile({
  label,
  value,
  detail,
  href,
  sparkline,
  gradientId,
  delta,
  delay = 0
}: {
  label: string;
  value: string;
  detail?: string;
  href: string;
  sparkline?: SparklinePoint[];
  gradientId?: string;
  delta?: string;
  delay?: number;
}) {
  return (
    <Link
      href={href}
      variant="current"
      className="group flex h-full w-full !no-underline outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-kumo-brand animate-fade-slide-in"
      style={rowDelay(delay)}
    >
      <div
        className={`flex h-full min-h-22 w-full flex-col gap-2 overflow-hidden bg-kumo-base px-4 pt-4 transition-colors group-hover:bg-kumo-tint group-focus-visible:bg-kumo-tint ${
          sparkline ? "pb-0" : "pb-4"
        }`}
      >
        <div className="flex items-center gap-1 text-xs font-medium text-kumo-subtle">
          {label}
        </div>
        <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
          <span className="text-xl font-semibold leading-none text-kumo-default">{value}</span>
          {detail ? <span className="text-sm font-medium text-kumo-subtle">{detail}</span> : null}
          {delta ? (
            <span className="inline-flex items-center gap-0.5 whitespace-nowrap text-xs font-semibold text-kumo-success">
              <TrendUpIcon className="size-3" />
              {delta}
            </span>
          ) : null}
        </div>
        {sparkline && gradientId ? (
          <div className="-mx-4 mt-auto w-[calc(100%+2rem)] min-w-0">
            <SparkArea data={sparkline} gradientId={gradientId} />
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
  href: string;
  value?: string;
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
  items
}: {
  title: string;
  count?: number;
  icon?: Icon;
  href?: string;
  actionHref?: string;
  items: ListWidgetItem[];
}) {
  return (
    <LayerCard className="flex h-full w-full flex-col">
      <WidgetHeader title={title} count={count} icon={icon} href={href} actionHref={actionHref} />
      <LayerCard.Primary className="flex-1 p-0">
        <div className="relative flex-1">
          <ul role="list" className="mx-3 flex flex-col divide-y divide-kumo-hairline">
            {items.map((item, index) => {
              const ItemIcon = item.icon;
              return (
                <li
                  key={`${item.label}-${index}`}
                  className="flex h-12 items-center justify-between gap-3 px-1 animate-fade-slide-in"
                  style={rowDelay(index)}
                >
                  <div className="flex min-w-0 flex-1 items-center gap-2">
                    {ItemIcon ? (
                      <ItemIcon className={`size-4 shrink-0 ${item.iconClassName ?? "text-kumo-subtle"}`} />
                    ) : null}
                    <div className="min-w-0 flex-1 overflow-hidden">
                      <Link
                        href={item.href}
                        variant="current"
                        target={item.external ? "_blank" : undefined}
                        rel={item.external ? "noreferrer" : undefined}
                        className="flex w-full min-w-0 items-center gap-2 overflow-hidden !no-underline !decoration-[0.1em] text-base font-medium text-kumo-default hover:!underline"
                      >
                        <span className="truncate">{item.label}</span>
                        {item.external ? <ArrowRightIcon className="size-4 shrink-0 text-kumo-subtle" /> : null}
                      </Link>
                      {item.detail ? <div className="truncate text-xs text-kumo-subtle">{item.detail}</div> : null}
                    </div>
                  </div>
                  {item.value ? (
                    <span className={`max-w-[40%] shrink-0 truncate whitespace-nowrap text-sm font-medium tabular-nums ${toneClass(item.valueTone ?? "subtle")}`} title={item.value}>
                      {item.value}
                    </span>
                  ) : null}
                </li>
              );
            })}
          </ul>
        </div>
      </LayerCard.Primary>
    </LayerCard>
  );
}

function NodesWidget({ nodes }: { nodes: AdminNode[] }) {
  const rows = nodes.slice(0, 4);
  return (
    <LayerCard className="flex h-full w-full flex-col">
      <WidgetHeader
        title="Nodes"
        count={nodes.length}
        href={adminPath("/nodes")}
        actionHref={adminPath("/nodes")}
      />
      <LayerCard.Primary className="flex-1 p-0">
        <div className="relative flex-1">
          {rows.length > 0 ? (
            <ul role="list" className="mx-3 flex flex-col divide-y divide-kumo-hairline">
              {rows.map((node, index) => {
                const status = nodeHealth(node);
                const StatusIcon = status.icon;
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
                          variant="current"
                          className="block max-w-full truncate bg-kumo-base pr-2 text-base font-medium text-kumo-default !no-underline !decoration-[0.1em] group-hover/row:!underline"
                          title={node.name}
                        >
                          {node.name}
                        </Link>
                      </div>
                      <div className="absolute right-0 bottom-0 flex h-8 w-[40%] min-w-0 items-center pb-px">
                        <SparkArea data={placeholderNodeSparkline(index)} gradientId={`node-sparkline-${index}`} />
                      </div>
                    </div>
                    <span className="truncate text-right text-sm text-kumo-subtle" title={formatNodeVersion(node)}>{formatNodeVersion(node)}</span>
                  </li>
                );
              })}
            </ul>
          ) : (
            <div className="flex h-full min-h-36 items-center justify-center text-sm text-kumo-subtle">
              No nodes.
            </div>
          )}
        </div>
      </LayerCard.Primary>
    </LayerCard>
  );
}

function buildUserItems(users: AdminUser[]): ListWidgetItem[] {
  if (users.length === 0) {
    return [{ label: "No users", href: adminPath("/users"), value: "—", icon: UsersIcon }];
  }
  return users.slice(0, 4).map((user) => ({
    label: user.display_name || user.name,
    href: adminPath("/users"),
    value: user.status,
    detail: `${user.proxy_count} proxies`,
    icon: user.status === "active" ? CheckCircleIcon : XCircleIcon,
    iconClassName: user.status === "active" ? "text-kumo-success" : "text-kumo-inactive",
    valueTone: user.status === "active" ? "success" : "subtle",
  }));
}

function buildTrafficItems(trafficUsers: TrafficByUser[]): ListWidgetItem[] {
  if (trafficUsers.length === 0) {
    return [{ label: "No traffic yet", href: adminPath("/traffic"), value: "—", icon: GaugeIcon }];
  }
  return trafficUsers.slice(0, 4).map((row) => ({
    label: row.user,
    href: adminPath("/traffic"),
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
      href: adminPath("/settings"),
      value: release?.boxfleet_version ?? "—",
      icon: HardDrivesIcon
    },
    {
      label: "sing-box target",
      href: adminPath("/settings"),
      value: release?.sing_box_version ?? "—",
      icon: ArrowsClockwiseIcon
    },
    {
      label: "System logs",
      href: adminPath("/system-logs"),
      value: formatCompactNumber(logs.length),
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
  if (logs.length === 0) {
    return [{ label: "No recent log events", href: adminPath("/system-logs"), value: "—", icon: ListChecksIcon }];
  }
  return logs.slice(0, 4).map((log) => ({
    label: log.message,
    href: adminPath("/system-logs"),
    value: log.level || log.service,
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
          href: adminPath("/nodes"),
          icon: HardDrivesIcon
        },
        {
          label: "Invite users and issue access",
          href: adminPath("/users"),
          icon: UsersIcon
        },
        {
          label: "Review recent network events",
          href: adminPath("/network-events"),
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

  return (
    <div className="flex min-h-full flex-col bg-kumo-canvas">
      <PageTopBar current="Account home" />
      <main className="w-full grow bg-kumo-canvas">
        <PageHeader
          title="BoxFleet Admin"
          description="Central control plane for nodes, users, traffic, and config versions."
          actions={
            <>
              <LinkButton href={adminPath("/nodes")} variant="primary" icon={PlusIcon}>
                Add
              </LinkButton>
            </>
          }
        />
        <div className="mx-auto flex w-full max-w-[1400px] flex-col px-6 md:gap-4 md:px-8 lg:px-10 xl:gap-6">
          <div className="pb-6 tabular-nums xl:pb-8">
            <div className="grid auto-rows-min grid-cols-6 gap-4">
              <div className="col-span-6">
                <section aria-label="Analytics" className="w-full space-y-3">
                  <div className="flex items-center justify-between gap-2">
                    <div>
                      <h2 className="text-base font-semibold text-kumo-default">Analytics</h2>
                      <p className="text-xs text-kumo-subtle">Trend lines are layout previews until historical telemetry is available.</p>
                    </div>
                    {/* TODO(time-series): enable this picker when its value drives the telemetry query. */}
                    <Button variant="secondary" icon={CalendarBlankIcon} disabled>
                      Last 24 hours
                    </Button>
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
                          href: adminPath("/nodes")
                        },
                        {
                          label: "System warnings",
                          value: formatCompactNumber(logs.filter((log) => latestLogTone(log) !== "subtle").length),
                          detail: `${logs.length} log events`,
                          href: adminPath("/system-logs")
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
                          href: adminPath("/traffic"),
                          sparkline: toSparkline(PLACEHOLDER_ANALYTICS_SPARKLINES.traffic),
                          gradientId: "traffic-analytics-sparkline"
                          // TODO(time-series): add delta="live" only after the sparkline is backed by telemetry.
                        },
                        {
                          label: "Traffic users",
                          value: formatCompactNumber(trafficUsers.length),
                          detail: `${activeUsers}/${users.length} active`,
                          href: adminPath("/traffic")
                        }
                      ]}
                    />
                    <AnalyticsCard
                      title="Activity"
                      icon={ChartLineUpIcon}
                      tiles={[
                        {
                          label: "Active nodes",
                          value: `${activeNodes}/${nodes.length}`,
                          href: adminPath("/nodes"),
                          sparkline: toSparkline(PLACEHOLDER_ANALYTICS_SPARKLINES.users),
                          gradientId: "nodes-analytics-sparkline"
                        },
                        {
                          label: "Recent logs",
                          value: formatCompactNumber(logs.length),
                          href: adminPath("/system-logs"),
                          sparkline: toSparkline(PLACEHOLDER_ANALYTICS_SPARKLINES.logs),
                          gradientId: "logs-analytics-sparkline"
                        }
                      ]}
                    />
                  </div>
                </section>
              </div>

              <OverviewGridItem>
                <NodesWidget nodes={nodes} />
              </OverviewGridItem>
              <OverviewGridItem>
                <SimpleListWidget
                  title="Users"
                  count={users.length}
                  icon={UsersIcon}
                  href={adminPath("/users")}
                  actionHref={adminPath("/users")}
                  items={buildUserItems(users)}
                />
              </OverviewGridItem>
              <OverviewGridItem>
                <SimpleListWidget
                  title="Traffic"
                  count={trafficUsers.length}
                  icon={GaugeIcon}
                  href={adminPath("/traffic")}
                  items={buildTrafficItems(trafficUsers)}
                />
              </OverviewGridItem>
              <OverviewGridItem>
                <SimpleListWidget
                  title="System"
                  icon={HardDrivesIcon}
                  href={adminPath("/settings")}
                  items={buildSystemItems(overview)}
                />
              </OverviewGridItem>
              <OverviewGridItem wide>
                <SimpleListWidget
                  title="System logs"
                  count={logs.length}
                  icon={ListChecksIcon}
                  href={adminPath("/system-logs")}
                  items={buildLogItems(logs)}
                />
              </OverviewGridItem>
              <OverviewGridItem>
                <NextStepsWidget />
              </OverviewGridItem>
            </div>
          </div>
        </div>
      </main>
    </div>
  );
}
