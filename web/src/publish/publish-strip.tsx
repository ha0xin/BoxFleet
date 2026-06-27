import { CheckCircleIcon, WarningCircleIcon, XCircleIcon, XIcon } from "@phosphor-icons/react";
import { Button, Loader } from "@cloudflare/kumo";

import { usePublishStatus } from "./publish-status";
import type { PublishStatus } from "./publish-status";

/**
 * Extra className applied to the 58px top bar container so the whole bar tints
 * with the current publish status. Returns "" for idle so the bar keeps its
 * default look. `applied` adds the slide-to-unlock sheen overlay.
 */
export function publishBarToneClass(status: PublishStatus): string {
  switch (status) {
    case "dirty":
    case "publishing":
    case "applying":
      return "bg-kumo-info-tint border-kumo-info/40";
    case "applied":
      return "bg-kumo-success-tint border-kumo-success/40 publish-bar-unlock";
    case "failed":
      return "bg-kumo-danger-tint border-kumo-danger/40";
    default:
      return "";
  }
}

/**
 * Right-aligned content of the global publish bar. Rendered inside the existing
 * top bars (PageTopBar / AppPageHeader); the bar background is tinted separately
 * via `publishBarToneClass`. Renders nothing when idle.
 */
export function PublishStrip() {
  const { status, changedCount, changesError, progress, openDiff, dismiss } = usePublishStatus();

  if (status === "idle") {
    // A failing /config/changes leaves status at idle; surface it instead of
    // silently implying every node is up to date.
    if (changesError) {
      return (
        <div className="flex items-center gap-1.5 text-sm font-medium text-kumo-warning" title={changesError}>
          <WarningCircleIcon className="size-4" weight="fill" />
          Config status unavailable
        </div>
      );
    }
    return null;
  }

  if (status === "dirty") {
    return (
      <div className="flex items-center gap-3 text-sm">
        <span className="flex items-center gap-1.5 font-medium text-kumo-info">
          <WarningCircleIcon className="size-4" weight="fill" />
          {changedCount} {changedCount === 1 ? "node has" : "nodes have"} unpublished changes
        </span>
        <Button size="sm" onClick={openDiff}>
          Review &amp; apply
        </Button>
      </div>
    );
  }

  if (status === "publishing" || status === "applying") {
    const label =
      status === "publishing"
        ? "Publishing…"
        : `Applying ${progress.applied}/${progress.total || "…"} nodes`;
    return (
      <div className="flex items-center gap-2 text-sm font-medium text-kumo-info">
        <Loader size={16} />
        {label}
      </div>
    );
  }

  if (status === "applied") {
    return (
      <div className="flex items-center gap-1.5 text-sm font-medium text-kumo-success">
        <CheckCircleIcon className="size-4" weight="fill" />
        All nodes up to date
      </div>
    );
  }

  // failed / incomplete
  const failedLabel =
    progress.failed > 0
      ? `Apply failed on ${progress.failed} node(s)`
      : progress.total > 0
        ? `Apply incomplete — ${progress.applied}/${progress.total} nodes responded`
        : "Publish failed";
  return (
    <div className="flex items-center gap-3 text-sm">
      <span className="flex items-center gap-1.5 font-medium text-kumo-danger">
        <XCircleIcon className="size-4" weight="fill" />
        {failedLabel}
      </span>
      <Button size="sm" variant="ghost" onClick={openDiff}>
        Review
      </Button>
      <Button size="sm" variant="ghost" shape="square" aria-label="Dismiss" onClick={dismiss}>
        <XIcon className="size-4" />
      </Button>
    </div>
  );
}
