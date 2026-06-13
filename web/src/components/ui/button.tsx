import * as React from "react";
import { Button as KumoButton, buttonVariants as kumoButtonVariants } from "@cloudflare/kumo/components/button";

import { cn } from "@/lib/utils";

type LegacyButtonVariant = "default" | "secondary" | "tertiary" | "error" | "warning";
type LegacyButtonSize = "tiny" | "sm" | "md" | "lg";

export interface ButtonProps
  extends Omit<React.ButtonHTMLAttributes<HTMLButtonElement>, "prefix"> {
  asChild?: boolean;
  shape?: "square" | "circle" | "rounded";
  svgOnly?: boolean;
  prefix?: React.ReactNode;
  suffix?: React.ReactNode;
  shadow?: boolean;
  loading?: boolean;
  variant?: LegacyButtonVariant | null;
  size?: LegacyButtonSize | null;
}

const variantMap = {
  default: "primary",
  secondary: "secondary",
  tertiary: "ghost",
  error: "destructive",
  warning: "secondary"
} as const;

const sizeMap = {
  tiny: "xs",
  sm: "sm",
  md: "base",
  lg: "lg"
} as const;

export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  (
    { className, variant, size, asChild, shape, svgOnly, prefix, suffix, shadow, loading, children, disabled, ...props },
    ref
  ) => {
    if (asChild && React.isValidElement(children)) {
      const child = children as React.ReactElement<{ className?: string }>;
      return React.cloneElement(children, {
        ...props,
        ref,
        className: cn(
          buttonVariants({ variant, size }),
          (shape === "square" || shape === "circle") && "aspect-square h-[unset] p-0",
          (shape === "rounded" || shape === "circle") && "rounded-full",
          svgOnly && "aspect-square px-0",
          shadow && "shadow-sm",
          className,
          child.props.className
        )
      } as React.HTMLAttributes<HTMLElement> & { ref: React.Ref<HTMLButtonElement> });
    }

    const kumoSize = sizeMap[size ?? "md"];
    const buttonClassName = cn(
      kumoButtonVariants({
        variant: variantMap[variant ?? "default"],
        size: kumoSize,
        shape: shape === "circle" ? "circle" : shape === "square" || svgOnly ? "square" : "base"
      }),
      variant === "warning" && "!bg-kumo-warning !text-black hover:!bg-kumo-warning/80",
      shape === "rounded" && "rounded-full",
      svgOnly && "aspect-square px-0",
      shadow && "shadow-sm",
      className
    );
    return (
      <KumoButton
        ref={ref}
        icon={loading ? undefined : prefix}
        loading={loading}
        disabled={disabled || loading}
        className={buttonClassName}
        {...props}
      >
        {children}
        {loading ? null : suffix}
      </KumoButton>
    );
  }
);
Button.displayName = "Button";

export function buttonVariants({
  variant,
  size,
  shape
}: {
  variant?: LegacyButtonVariant | null;
  size?: LegacyButtonSize | null;
  shape?: ButtonProps["shape"];
} = {}) {
  return cn(
    kumoButtonVariants({
      variant: variantMap[variant ?? "default"],
      size: sizeMap[size ?? "md"],
      shape: shape === "circle" ? "circle" : shape === "square" ? "square" : "base"
    }),
    variant === "warning" && "!bg-kumo-warning !text-black hover:!bg-kumo-warning/80"
  );
}
