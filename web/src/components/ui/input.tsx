import * as React from "react";
import {
  Input as KumoInput,
  InputGroup,
  type InputProps as KumoInputProps
} from "@cloudflare/kumo/components/input";

import { cn } from "@/lib/utils";

type LegacyInputSize = "sm" | "md" | "lg";

export interface InputProps extends Omit<KumoInputProps, "size" | "prefix"> {
  prefix?: React.ReactNode;
  suffix?: React.ReactNode;
  prefixStyling?: boolean;
  suffixStyling?: boolean;
  label?: string;
  containerClassName?: string;
  size?: LegacyInputSize | null;
}

const sizeMap = {
  sm: "sm",
  md: "base",
  lg: "lg"
} as const;

export const Input = React.forwardRef<HTMLInputElement, InputProps>(
  (
    {
      className,
      containerClassName,
      size,
      label,
      suffix,
      prefix,
      prefixStyling = true,
      suffixStyling = true,
      "aria-label": ariaLabel,
      ...props
    },
    ref
  ) => {
    const kumoSize = sizeMap[size ?? "md"];
    const accessibleName = ariaLabel ?? (typeof label === "string" ? label : props.placeholder);

    if (prefix || suffix) {
      return (
        <InputGroup
          label={label}
          size={kumoSize}
          className={cn("w-full", containerClassName)}
          disabled={props.disabled}
        >
          {prefix ? (
            <InputGroup.Addon
              align="start"
              className={cn(!prefixStyling && "bg-transparent")}
            >
              {prefix}
            </InputGroup.Addon>
          ) : null}
          <InputGroup.Input
            ref={ref}
            className={className}
            aria-label={accessibleName}
            {...props}
          />
          {suffix ? (
            suffixStyling ? (
              <InputGroup.Suffix>{suffix}</InputGroup.Suffix>
            ) : (
              <InputGroup.Addon align="end">{suffix}</InputGroup.Addon>
            )
          ) : null}
        </InputGroup>
      );
    }

    return (
      <KumoInput
        ref={ref}
        label={label}
        size={kumoSize}
        aria-label={accessibleName}
        className={cn("w-full", className, containerClassName)}
        {...props}
      />
    );
  }
);
Input.displayName = "Input";
