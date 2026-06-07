import * as React from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";
import { Spinner } from "./spinner";

const buttonVariants = cva(
  "transition-colors select-none font-medium border border-transparent inline-flex justify-center items-center gap-0.5 max-w-full disabled:bg-gray-100 disabled:text-gray-700 disabled:border-gray-400 disabled:cursor-not-allowed",
  {
    variants: {
      variant: {
        default: "bg-gray-1000 text-background-100 hover:bg-gray-900",
        secondary:
          "bg-background-100 border-gray-alpha-400 text-gray-1000 hover:bg-gray-alpha-200",
        tertiary:
          "bg-transparent border-transparent text-gray-1000 hover:bg-gray-alpha-200",
        error: "bg-red-800 border-red-800 text-white hover:bg-red-900 hover:border-red-900",
        warning: "bg-amber-800 border-amber-800 text-[#0a0a0a] hover:bg-amber-900 hover:border-amber-900"
      },
      size: {
        tiny: "h-6 px-1.5 rounded text-xs leading-4",
        sm: "h-8 px-2 rounded-md text-sm leading-5",
        md: "h-10 px-3 rounded-md text-sm leading-5",
        lg: "h-12 px-3.5 rounded-lg text-base leading-6"
      }
    },
    defaultVariants: { variant: "default", size: "md" }
  }
);

export interface ButtonProps
  extends Omit<React.ButtonHTMLAttributes<HTMLButtonElement>, "prefix">,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean;
  shape?: "square" | "circle" | "rounded";
  svgOnly?: boolean;
  prefix?: React.ReactNode;
  suffix?: React.ReactNode;
  shadow?: boolean;
  loading?: boolean;
}

export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  (
    { className, variant, size, asChild, shape, svgOnly, prefix, suffix, shadow, loading, children, disabled, ...props },
    ref
  ) => {
    const Comp: any = asChild ? Slot : "button";
    return (
      <Comp
        ref={ref}
        disabled={disabled || loading}
        className={cn(
          buttonVariants({ variant, size }),
          (shape === "square" || shape === "circle") && "aspect-square h-[unset] p-0",
          (shape === "rounded" || shape === "circle") && "rounded-full",
          svgOnly && "aspect-square px-0",
          shadow && "shadow-small",
          className
        )}
        {...props}
      >
        {loading ? <Spinner size={size === "lg" ? 22 : 14} /> : prefix}
        {children ? <span className="px-1">{children}</span> : null}
        {loading ? null : suffix}
      </Comp>
    );
  }
);
Button.displayName = "Button";

export { buttonVariants };
