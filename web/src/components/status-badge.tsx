import type { ReactNode } from "react";
import { CheckIcon, InfoIcon, MinusIcon, WarningIcon, XIcon } from "@phosphor-icons/react";
import { Badge } from "@cloudflare/kumo";

/**
 * Canonical status rendering: a Kumo dot Badge, presented as icon plus text
 * inside resource tables, matching Domains. Log tables keep the compact Badge.
 * Tones map to Badge variants; `success`/`warning`/`error`/`neutral` get the
 * colored dot, `info` renders as a plain info badge (Kumo shows no dot for it).
 */
export type StatusTone = "success" | "warning" | "error" | "neutral" | "info";

export function StatusBadge({ tone, children }: { tone: StatusTone; children: ReactNode }) {
  const Icon = tone === "success" ? CheckIcon : tone === "warning" ? WarningIcon : tone === "error" ? XIcon : tone === "info" ? InfoIcon : MinusIcon;
  return (
    <Badge variant={tone} appearance="dot" className={`bf-status-badge bf-status-${tone} whitespace-nowrap`}>
      <Icon className="bf-status-icon hidden size-3.5 shrink-0" aria-hidden="true" />
      {children}
    </Badge>
  );
}
