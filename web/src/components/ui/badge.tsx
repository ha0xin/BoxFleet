import * as React from "react";
import { Badge as KumoBadge } from "@cloudflare/kumo/components/badge";

import { cn } from "@/lib/utils";

type LegacyBadgeVariant =
  | "gray"
  | "gray-subtle"
  | "blue"
  | "blue-subtle"
  | "purple"
  | "purple-subtle"
  | "amber"
  | "amber-subtle"
  | "red"
  | "red-subtle"
  | "pink"
  | "pink-subtle"
  | "green"
  | "green-subtle"
  | "teal"
  | "teal-subtle";

type LegacyBadgeSize = "sm" | "md" | "lg";

export interface BadgeProps extends Omit<React.HTMLAttributes<HTMLSpanElement>, "color"> {
  icon?: React.ReactNode;
  variant?: LegacyBadgeVariant | null;
  size?: LegacyBadgeSize | null;
}

type KumoBadgeVariant = React.ComponentProps<typeof KumoBadge>["variant"];

const variantMap: Record<LegacyBadgeVariant, KumoBadgeVariant> = {
  gray: "neutral",
  "gray-subtle": "secondary",
  blue: "blue",
  "blue-subtle": "info",
  purple: "purple",
  "purple-subtle": "secondary",
  amber: "orange",
  "amber-subtle": "warning",
  red: "red",
  "red-subtle": "error",
  pink: "purple",
  "pink-subtle": "secondary",
  green: "green",
  "green-subtle": "success",
  teal: "teal",
  "teal-subtle": "teal-subtle"
};

const sizeClass: Record<LegacyBadgeSize, string> = {
  sm: "px-1.5 py-0 text-[11px]",
  md: "",
  lg: "px-3 py-1 text-sm"
};

export function Badge({ className, variant = "gray-subtle", size = "md", icon, children, ...props }: BadgeProps) {
  return (
    <KumoBadge
      variant={variantMap[variant ?? "gray-subtle"]}
      className={cn(sizeClass[size ?? "md"], className)}
      {...props}
    >
      {icon ? <span className="inline-flex items-center">{icon}</span> : null}
      {children}
    </KumoBadge>
  );
}

export function badgeVariants({ variant = "gray-subtle", size = "md" }: Pick<BadgeProps, "variant" | "size"> = {}) {
  return cn(sizeClass[size ?? "md"], variant ? "" : "");
}
