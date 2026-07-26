import type { ReactNode } from "react";
import { Badge } from "@cloudflare/kumo";

/**
 * Canonical status rendering for table cells and widgets: a Kumo dot Badge.
 * Tones map to Badge variants; `success`/`warning`/`error`/`neutral` get the
 * colored dot, `info` renders as a plain info badge (Kumo shows no dot for it).
 */
export type StatusTone = "success" | "warning" | "error" | "neutral" | "info";

export function StatusBadge({ tone, children }: { tone: StatusTone; children: ReactNode }) {
  return (
    <Badge variant={tone} appearance="dot" className="whitespace-nowrap">
      {children}
    </Badge>
  );
}
