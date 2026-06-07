import * as React from "react";

import { cn } from "@/lib/utils";

export type StatusTone =
  | "gray"
  | "blue"
  | "green"
  | "teal"
  | "amber"
  | "red"
  | "purple"
  | "pink";

const toneClass: Record<StatusTone, string> = {
  gray: "bg-gray-600",
  blue: "bg-blue-700",
  green: "bg-green-700",
  teal: "bg-teal-700",
  amber: "bg-amber-700",
  red: "bg-red-800",
  purple: "bg-purple-700",
  pink: "bg-pink-700"
};

interface StatusDotProps {
  tone?: StatusTone;
  pulse?: boolean;
  className?: string;
}

export function StatusDot({ tone = "gray", pulse, className }: StatusDotProps) {
  return (
    <span className={cn("relative inline-flex h-2.5 w-2.5 shrink-0", className)}>
      {pulse ? (
        <span className={cn("absolute inset-0 animate-ping rounded-full opacity-75", toneClass[tone])} />
      ) : null}
      <span className={cn("relative inline-flex h-2.5 w-2.5 rounded-full", toneClass[tone])} />
    </span>
  );
}
