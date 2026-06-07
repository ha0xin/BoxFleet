import * as React from "react";

import { cn } from "@/lib/utils";

export const Textarea = React.forwardRef<HTMLTextAreaElement, React.TextareaHTMLAttributes<HTMLTextAreaElement>>(
  ({ className, ...props }, ref) => (
    <textarea
      ref={ref}
      className={cn(
        "flex w-full rounded-md border border-gray-alpha-400 bg-background-100 px-3 py-2 text-sm text-gray-1000 transition-shadow duration-150",
        "placeholder:text-gray-700 focus:outline-none focus-visible:shadow-input-ring",
        "disabled:cursor-not-allowed disabled:bg-background-200 disabled:text-gray-700",
        className
      )}
      {...props}
    />
  )
);
Textarea.displayName = "Textarea";
