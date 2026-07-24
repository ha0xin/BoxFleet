import type { CSSProperties, ReactNode } from "react";
import {
  ArrowRightIcon,
  ListChecksIcon,
  PlusIcon,
  WarningCircleIcon,
  XCircleIcon,
  CheckCircleIcon
} from "@phosphor-icons/react";
import type { Icon } from "@phosphor-icons/react";
import { Area, AreaChart, ResponsiveContainer } from "recharts";
import { Badge, Breadcrumbs, LayerCard, Link, LinkButton, Sidebar } from "@cloudflare/kumo";

import { adminBasename } from "@/navigation";
import { usePublishStatus } from "@/publish/publish-status";
import { PublishStrip, publishBarToneClass } from "@/publish/publish-strip";
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
      <ResponsiveContainer width="100%" height="100%" initialDimension={{ width: 320, height: 32 }}>
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
  const { status } = usePublishStatus();
  return (
    <div
      className={`flex min-h-[58px] shrink-0 flex-wrap items-center justify-between gap-2 border-b border-kumo-line px-4 py-2 transition-colors duration-300 sm:px-6 ${publishBarToneClass(status)}`}
    >
      <div className="flex min-w-0 items-center gap-2">
        <Sidebar.Trigger className="md:hidden" />
        <Breadcrumbs size="sm">
          <Breadcrumbs.Link href={adminPath("/")}>BoxFleet</Breadcrumbs.Link>
          <Breadcrumbs.Separator />
          <Breadcrumbs.Current>{current}</Breadcrumbs.Current>
        </Breadcrumbs>
      </div>
      <div className="ml-auto flex min-w-0 flex-wrap items-center justify-end gap-2 sm:gap-3">
        <PublishStrip />
        <div className="flex gap-1">
          <LinkButton href={adminPath("/system-logs")} variant="ghost" size="sm" icon={ListChecksIcon}>
            <span className="hidden md:inline">Logs</span>
          </LinkButton>
        </div>
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
      <header className="mb-4 flex flex-wrap items-start justify-between gap-4 pt-6">
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
  actionLabel = "Add"
}: {
  title: string;
  count?: number;
  icon?: Icon;
  href?: string;
  actionHref?: string;
  actionLabel?: string;
}) {
  return (
    <LayerCard.Secondary className="h-14 justify-between py-0">
      <div role="heading" aria-level={2} className="flex min-w-0 items-center gap-2">
        {Icon ? <Icon className="size-4.5 shrink-0" /> : null}
        <span className="truncate">{title}</span>
        {typeof count === "number" ? <Badge variant="secondary">{count}</Badge> : null}
      </div>
      <div className="flex shrink-0 items-center justify-center gap-1.5">
        {actionHref ? (
          <LinkButton href={actionHref} variant="secondary" size="sm" shape="square" aria-label={actionLabel}>
            <PlusIcon className="size-4" />
          </LinkButton>
        ) : null}
        {href ? (
          <Link href={href} variant="current" aria-label={`Open ${title}`} className="flex !no-underline text-kumo-default">
            <ArrowRightIcon className="pointer-events-none size-4 shrink-0" />
          </Link>
        ) : null}
      </div>
    </LayerCard.Secondary>
  );
}
