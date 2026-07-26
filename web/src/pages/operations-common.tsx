import type { CSSProperties, MouseEvent } from "react";
import { useNavigate } from "react-router-dom";
import {
  ArrowRightIcon,
  PlusIcon,
  WarningCircleIcon,
  XCircleIcon,
  CheckCircleIcon
} from "@phosphor-icons/react";
import type { Icon } from "@phosphor-icons/react";
import { Badge, LayerCard, Link, LinkButton } from "@cloudflare/kumo";

import { adminBasename } from "@/navigation";
import type { AdminNode } from "../types";

export type Tone = "default" | "subtle" | "success" | "warning" | "danger";
export type BadgeTone = "success" | "warning" | "error" | "neutral" | "info";

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
  const current = node.current_version || "n/a";
  const target = node.target_version || current;
  return current === target ? current : `${current} -> ${target}`;
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
  const navigate = useNavigate();
  const spaNavigate = (path: string) => (event: MouseEvent<HTMLElement>) => {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    navigate(path);
  };
  return (
    <LayerCard.Secondary className="h-14 justify-between py-0">
      <h2 className="flex min-w-0 items-center gap-2 text-[length:inherit] font-[number:inherit]">
        {Icon ? <Icon className="size-4.5 shrink-0" /> : null}
        <span className="truncate">{title}</span>
        {typeof count === "number" ? <Badge variant="secondary">{count}</Badge> : null}
      </h2>
      <div className="flex shrink-0 items-center justify-center gap-1.5">
        {actionHref ? (
          <LinkButton
            href={adminPath(actionHref)}
            onClick={spaNavigate(actionHref)}
            variant="secondary"
            size="sm"
            shape="square"
            aria-label={actionLabel}
          >
            <PlusIcon className="size-4" />
          </LinkButton>
        ) : null}
        {href ? (
          <Link
            href={adminPath(href)}
            onClick={spaNavigate(href)}
            variant="current"
            aria-label={`Open ${title}`}
            className="flex !no-underline text-kumo-default"
          >
            <ArrowRightIcon className="pointer-events-none size-4 shrink-0" />
          </Link>
        ) : null}
      </div>
    </LayerCard.Secondary>
  );
}
