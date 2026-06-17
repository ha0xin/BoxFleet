import type { CSSProperties, ReactNode } from "react";
import {
  ArrowRightIcon,
  DotsThreeIcon,
  LightningIcon,
  ListChecksIcon,
  PlusIcon,
  StarIcon,
  UsersIcon,
  WarningCircleIcon,
  XCircleIcon,
  CheckCircleIcon
} from "@phosphor-icons/react";
import type { Icon } from "@phosphor-icons/react";
import { Area, AreaChart, ResponsiveContainer } from "recharts";
import { Badge, Breadcrumbs, Button, LayerCard, Link, LinkButton } from "@cloudflare/kumo";

import { adminBasename } from "@/navigation";
import type { AdminNode } from "../types";

export type SparklinePoint = { index: number; value: number };
export type Tone = "default" | "subtle" | "success" | "warning" | "danger";
export type BadgeTone = "success" | "warning" | "error" | "neutral" | "info" | "secondary";

const SPARKLINE_COLOR = "var(--color-kumo-info)";

export const rowLinkClassName =
  "min-w-0 text-base font-medium text-kumo-default !no-underline !decoration-[0.1em] hover:!underline group-hover/row:!underline";

export function adminPath(path: string): string {
  return `${adminBasename()}${path}`;
}

export function formatCompactNumber(value: number): string {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`;
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`;
  return `${value}`;
}

export function formatRelativeTime(value: string): string {
  if (!value) return "never";
  const time = new Date(value).getTime();
  if (!Number.isFinite(time)) return value;
  const seconds = Math.max(0, Math.floor((Date.now() - time) / 1000));
  if (seconds < 60) return "just now";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

export function rowDelay(index: number, step = 50): CSSProperties {
  return { "--row-delay": `${index * step}ms` } as CSSProperties;
}

export function toSparkline(values: readonly number[]): SparklinePoint[] {
  return values.map((value, index) => ({ index, value }));
}

export function toneClass(tone: Tone = "default"): string {
  switch (tone) {
    case "success":
      return "text-kumo-success";
    case "warning":
      return "text-kumo-warning";
    case "danger":
      return "text-kumo-danger";
    case "subtle":
      return "text-kumo-subtle";
    default:
      return "text-kumo-default";
  }
}

export function isNodeOnline(node: AdminNode): boolean {
  return node.status === "active";
}

export function isNodeDrifting(node: AdminNode): boolean {
  return Boolean(node.target_version && node.current_version && node.target_version !== node.current_version);
}

export function nodeHealth(node: AdminNode): {
  label: string;
  icon: Icon;
  className: string;
  badgeTone: BadgeTone;
} {
  if (node.status === "disabled") {
    return { label: "Disabled", icon: XCircleIcon, className: "text-kumo-inactive", badgeTone: "neutral" };
  }
  if (node.status === "degraded" || node.apply_status === "failed") {
    return { label: "Needs attention", icon: XCircleIcon, className: "text-kumo-danger", badgeTone: "error" };
  }
  if (
    node.status === "pending" ||
    node.apply_status === "pending" ||
    node.apply_status === "rolled_back" ||
    isNodeDrifting(node)
  ) {
    return { label: "Pending config", icon: WarningCircleIcon, className: "text-kumo-warning", badgeTone: "warning" };
  }
  return { label: "Online", icon: CheckCircleIcon, className: "text-kumo-success", badgeTone: "success" };
}

export function formatNodeVersion(node: AdminNode): string {
  const current = node.current_version || node.sing_box_version || "n/a";
  const target = node.target_version || current;
  return current === target ? current : `${current} -> ${target}`;
}

export function SparkArea({
  data,
  gradientId,
  className = "h-8 w-full min-w-0"
}: {
  data: SparklinePoint[];
  gradientId: string;
  className?: string;
}) {
  return (
    <div className={`pointer-events-none ${className}`} aria-hidden="true">
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={data} margin={{ top: 1, right: 0, bottom: 0, left: 0 }}>
          <defs>
            <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
              <stop offset="5%" stopColor={SPARKLINE_COLOR} stopOpacity={0.55} />
              <stop offset="95%" stopColor={SPARKLINE_COLOR} stopOpacity={0} />
            </linearGradient>
          </defs>
          <Area
            type="monotone"
            dataKey="value"
            stroke={SPARKLINE_COLOR}
            strokeWidth={1.4}
            fill={`url(#${gradientId})`}
            fillOpacity={0.35}
            dot={false}
            isAnimationActive={false}
          />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}

export function PageTopBar({ current }: { current: string }) {
  return (
    <div className="flex h-[58px] shrink-0 items-center justify-between gap-4 border-b border-kumo-line px-6">
      <Breadcrumbs size="sm">
        <Breadcrumbs.Link href={adminPath("/")}>BoxFleet</Breadcrumbs.Link>
        <Breadcrumbs.Separator />
        <Breadcrumbs.Current>{current}</Breadcrumbs.Current>
      </Breadcrumbs>
      <div className="ml-auto flex gap-1">
        <Button variant="ghost" size="sm" icon={LightningIcon}>
          <span className="hidden md:inline">Ask AI</span>
        </Button>
        <LinkButton href={adminPath("/system-logs")} variant="ghost" size="sm" icon={ListChecksIcon}>
          <span className="hidden md:inline">Logs</span>
        </LinkButton>
        <Button variant="ghost" size="sm" shape="square" aria-label="User menu">
          <UsersIcon className="size-4 text-kumo-subtle" />
        </Button>
      </div>
    </div>
  );
}

export function PageHeader({
  title,
  description,
  actions
}: {
  title: string;
  description: string;
  actions?: ReactNode;
}) {
  return (
    <div className="mx-auto w-full max-w-[1400px] px-6 py-0 md:px-8 lg:px-10">
      <div className="flex items-center gap-2">
        <nav className="mr-4 flex h-12 min-w-0 grow items-center gap-1 overflow-hidden whitespace-nowrap text-base" aria-label="breadcrumb">
          <div className="flex min-w-0 max-w-full items-center gap-1 font-medium" aria-current="page">
            <span className="truncate">{title}</span>
          </div>
        </nav>
      </div>
      <header className="mb-4 flex flex-wrap items-start justify-between gap-4">
        <div className="flex min-w-0 flex-col">
          <h1 className="mb-1.5 text-xl font-semibold text-kumo-default md:text-3xl">{title}</h1>
          <p className="max-w-2xl text-base leading-5 text-kumo-subtle lg:text-lg">{description}</p>
        </div>
        {actions ? <div className="flex shrink-0 items-center gap-2">{actions}</div> : null}
      </header>
    </div>
  );
}

export function WidgetHeader({
  title,
  count,
  icon: Icon,
  href,
  actionHref,
  actionLabel = "Add",
  starred = false
}: {
  title: string;
  count?: number;
  icon?: Icon;
  href?: string;
  actionHref?: string;
  actionLabel?: string;
  starred?: boolean;
}) {
  return (
    <LayerCard.Secondary className="h-14 justify-between py-0">
      <div role="heading" aria-level={2} className="flex min-w-0 items-center gap-2">
        {Icon ? <Icon className="size-4.5 shrink-0" /> : null}
        <span className="truncate">{title}</span>
        {typeof count === "number" ? <Badge variant="secondary">{count}</Badge> : null}
      </div>
      <div className="flex shrink-0 items-center justify-center gap-1.5">
        {starred ? (
          <Button variant="secondary" size="sm" shape="square" aria-label="Starred">
            <StarIcon className="size-4" />
          </Button>
        ) : null}
        {actionHref ? (
          <LinkButton href={actionHref} variant="secondary" size="sm" shape="square" aria-label={actionLabel}>
            <PlusIcon className="size-4" />
          </LinkButton>
        ) : null}
        <Button variant="ghost" size="sm" shape="square" aria-label={`${title} actions`}>
          <DotsThreeIcon className="size-4" />
        </Button>
        {href ? (
          <Link href={href} variant="current" aria-label={`Open ${title}`} className="flex !no-underline text-kumo-default">
            <ArrowRightIcon className="pointer-events-none size-4 shrink-0" />
          </Link>
        ) : null}
      </div>
    </LayerCard.Secondary>
  );
}

export function SplitMetricCard({
  title,
  icon,
  tiles
}: {
  title: string;
  icon: Icon;
  tiles: Array<{
    label: string;
    value: string;
    detail?: string;
    tone?: Tone;
    sparkline?: SparklinePoint[];
    gradientId?: string;
  }>;
}) {
  return (
    <LayerCard className="flex h-full w-full flex-col">
      <WidgetHeader title={title} icon={icon} />
      <LayerCard.Primary className="flex-1 p-0">
        <div className="grid h-full auto-rows-fr grid-cols-1 sm:grid-cols-2">
          {tiles.map((tile, index) => (
            <div key={tile.label} className={index === 0 ? "border-b border-kumo-line sm:border-r sm:border-b-0" : ""}>
              <div className="flex h-full min-h-22 flex-col gap-2 overflow-hidden bg-kumo-base px-4 pt-4 pb-4 animate-fade-slide-in" style={rowDelay(index)}>
                <div className="text-xs font-medium text-kumo-subtle">{tile.label}</div>
                <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
                  <span className={`text-xl font-semibold leading-none ${toneClass(tile.tone)}`}>{tile.value}</span>
                  {tile.detail ? <span className="text-sm font-medium text-kumo-subtle">{tile.detail}</span> : null}
                </div>
                {tile.sparkline && tile.gradientId ? (
                  <div className="-mx-4 mt-auto w-[calc(100%+2rem)] min-w-0">
                    <SparkArea data={tile.sparkline} gradientId={tile.gradientId} />
                  </div>
                ) : null}
              </div>
            </div>
          ))}
        </div>
      </LayerCard.Primary>
    </LayerCard>
  );
}

export function EmptyState({ children }: { children: ReactNode }) {
  return <div className="flex min-h-36 items-center justify-center px-4 py-8 text-sm text-kumo-subtle">{children}</div>;
}
