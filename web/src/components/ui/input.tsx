import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

const inputVariants = cva(
  "flex border border-gray-alpha-400 overflow-hidden bg-background-100 relative focus-within:shadow-input-ring transition-shadow duration-150",
  {
    variants: {
      size: {
        sm: "h-8 text-sm rounded-md [--icon-size:14px]",
        md: "h-10 text-sm rounded-md [--icon-size:16px]",
        lg: "h-12 text-base rounded-lg [--icon-size:18px]"
      }
    },
    defaultVariants: { size: "md" }
  }
);

export interface InputProps
  extends Omit<React.InputHTMLAttributes<HTMLInputElement>, "size" | "prefix">,
    VariantProps<typeof inputVariants> {
  prefix?: React.ReactNode;
  suffix?: React.ReactNode;
  prefixStyling?: boolean;
  suffixStyling?: boolean;
  label?: string;
  containerClassName?: string;
}

export const Input = React.forwardRef<HTMLInputElement, InputProps>(
  (
    { className, containerClassName, type, size, label, suffix, prefix, prefixStyling = true, suffixStyling = true, id, ...props },
    ref
  ) => {
    const autoId = React.useId();
    const inputId = id ?? autoId;

    const field = (
      <div className={cn(inputVariants({ size }), containerClassName)}>
        {prefix ? (
          <span
            className={cn(
              "flex shrink-0 items-center px-3 text-gray-700 [&>svg]:h-[var(--icon-size)] [&>svg]:w-[var(--icon-size)]",
              prefixStyling && "bg-background-200"
            )}
          >
            {prefix}
          </span>
        ) : null}
        <input
          id={inputId}
          type={type}
          ref={ref}
          className={cn(
            "w-full bg-transparent outline-none placeholder:text-gray-700 disabled:cursor-not-allowed disabled:bg-background-200 disabled:text-gray-700 disabled:placeholder:text-gray-500",
            (!prefix || prefixStyling) && "pl-3",
            (!suffix || suffixStyling) && "pr-3",
            className
          )}
          {...props}
        />
        {suffix ? (
          <span
            className={cn(
              "flex shrink-0 items-center px-3 text-gray-700 [&>svg]:h-[var(--icon-size)] [&>svg]:w-[var(--icon-size)]",
              suffixStyling && "bg-background-200"
            )}
          >
            {suffix}
          </span>
        ) : null}
      </div>
    );

    if (!label) return field;
    return (
      <label htmlFor={inputId} className="block">
        <div className="mb-2 text-xs text-gray-900">{label}</div>
        {field}
      </label>
    );
  }
);
Input.displayName = "Input";
