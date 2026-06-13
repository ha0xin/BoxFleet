import * as React from "react";
import { Banner } from "@cloudflare/kumo/components/banner";

import { cn } from "@/lib/utils";

type NoteVariant = "secondary" | "success" | "error" | "warning" | "violet" | "cyan";
type NoteSize = "sm" | "md" | "lg";

interface NoteProps {
  children: React.ReactNode;
  action?: React.ReactNode;
  fill?: boolean;
  className?: string;
  variant?: NoteVariant | null;
  size?: NoteSize | null;
}

type KumoBannerVariant = React.ComponentProps<typeof Banner>["variant"];

const variantMap: Record<NoteVariant, KumoBannerVariant> = {
  secondary: "secondary",
  success: "default",
  error: "error",
  warning: "alert",
  violet: "secondary",
  cyan: "default"
};

const sizeClass: Record<NoteSize, string> = {
  sm: "px-3 py-2 text-sm",
  md: "px-4 py-3 text-base",
  lg: "px-5 py-4 text-base"
};

export function Note({ action, children, size = "md", variant = "secondary", fill, className }: NoteProps) {
  // Kumo's Banner only renders `action` in structured mode (when `title`/`description`
  // is set); passing content via `children` puts it in the bare layout that drops the
  // action. Always feed content through `description` so `action` is rendered.
  return (
    <Banner
      variant={variantMap[variant ?? "secondary"]}
      className={cn(sizeClass[size ?? "md"], fill && "ring ring-kumo-hairline", className)}
      description={children}
      action={action}
    />
  );
}
