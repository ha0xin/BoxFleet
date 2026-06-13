import * as React from "react";
import {
  Textarea as KumoTextarea,
  type InputAreaProps
} from "@cloudflare/kumo/components/input";

import { cn } from "@/lib/utils";

export const Textarea = React.forwardRef<HTMLTextAreaElement, InputAreaProps>(
  ({ className, ...props }, ref) => (
    <KumoTextarea
      ref={ref}
      className={cn("w-full", className)}
      aria-label={props["aria-label"] ?? props.placeholder ?? "Text area"}
      {...props}
    />
  )
);
Textarea.displayName = "Textarea";
