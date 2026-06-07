import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

const badgeVariants = cva(
  "inline-flex items-center justify-center rounded-full whitespace-nowrap font-medium",
  {
    variants: {
      variant: {
        gray: "bg-gray-700 text-white",
        "gray-subtle": "bg-gray-200 text-gray-1000",
        blue: "bg-blue-700 text-white",
        "blue-subtle": "bg-blue-200 text-blue-900",
        purple: "bg-purple-700 text-white",
        "purple-subtle": "bg-purple-200 text-purple-900",
        amber: "bg-amber-700 text-black",
        "amber-subtle": "bg-amber-200 text-amber-900",
        red: "bg-red-700 text-white",
        "red-subtle": "bg-red-200 text-red-900",
        pink: "bg-pink-700 text-white",
        "pink-subtle": "bg-pink-300 text-pink-900",
        green: "bg-green-700 text-white",
        "green-subtle": "bg-green-200 text-green-900",
        teal: "bg-teal-700 text-white",
        "teal-subtle": "bg-teal-300 text-teal-900"
      },
      size: {
        sm: "px-1.5 h-5 text-[11px] gap-0.5 [&_svg]:w-[11px] [&_svg]:h-[11px]",
        md: "px-2.5 h-6 text-xs gap-1 [&_svg]:w-[14px] [&_svg]:h-[14px]",
        lg: "px-3 h-8 text-sm gap-1.5 [&_svg]:w-4 [&_svg]:h-4"
      }
    },
    defaultVariants: { variant: "gray-subtle", size: "md" }
  }
);

export interface BadgeProps
  extends React.HTMLAttributes<HTMLSpanElement>,
    VariantProps<typeof badgeVariants> {
  icon?: React.ReactNode;
}

export function Badge({ className, variant, size, icon, children, ...props }: BadgeProps) {
  return (
    <span className={cn(badgeVariants({ variant, size }), className)} {...props}>
      {icon ? <span className="inline-flex items-center">{icon}</span> : null}
      {children}
    </span>
  );
}

export { badgeVariants };
