import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { AlertOctagon, AlertTriangle, CheckCircle2, Info } from "lucide-react";

import { cn } from "@/lib/utils";

const noteVariants = cva(
  "border p-2 flex leading-normal justify-between items-center rounded-md gap-3 [word-break:break-word] grow",
  {
    variants: {
      variant: {
        secondary:
          "text-gray-1000 border-gray-300 [--note-filled-bg:var(--ds-gray-alpha-200)] [--note-filled-border:transparent]",
        success:
          "text-blue-900 border-blue-400 selection:bg-blue-700 [--note-filled-bg:hsl(var(--ds-blue-200))] [--note-filled-border:hsl(var(--ds-blue-100))]",
        error:
          "text-red-900 border-red-400 selection:bg-red-800 [--note-filled-bg:hsl(var(--ds-red-200))] [--note-filled-border:hsl(var(--ds-red-100))]",
        warning:
          "text-amber-900 border-amber-400 selection:bg-amber-500 [--note-filled-bg:hsl(var(--ds-amber-200))] [--note-filled-border:hsl(var(--ds-amber-100))]",
        violet:
          "text-purple-900 border-purple-400 selection:bg-purple-600 [--note-filled-bg:hsl(var(--ds-purple-200))] [--note-filled-border:hsl(var(--ds-purple-100))]",
        cyan:
          "text-teal-900 border-teal-400 selection:bg-teal-900 [--note-filled-bg:hsl(var(--ds-teal-200))] [--note-filled-border:hsl(var(--ds-teal-100))]"
      },
      size: {
        sm: "py-1.5 px-2 text-[13px] min-h-[34px]",
        md: "py-2 px-3 text-sm min-h-[40px]",
        lg: "py-[11px] px-3 text-base min-h-[24px]"
      }
    },
    defaultVariants: { variant: "secondary", size: "md" }
  }
);

const icons: Record<string, typeof Info> = {
  success: CheckCircle2,
  error: AlertOctagon,
  warning: AlertTriangle
};

interface NoteProps extends VariantProps<typeof noteVariants> {
  children: React.ReactNode;
  action?: React.ReactNode;
  fill?: boolean;
  className?: string;
}

export function Note({ action, children, size, variant = "secondary", fill, className }: NoteProps) {
  const Icon = (variant && icons[variant]) || Info;
  return (
    <div
      className={cn(
        noteVariants({ variant, size }),
        fill && "border-[var(--note-filled-border)] bg-[var(--note-filled-bg)]",
        className
      )}
    >
      <span className="flex items-center gap-3">
        <Icon className="h-4 w-4 shrink-0" />
        <span>{children}</span>
      </span>
      {action ? <div className="shrink-0">{action}</div> : null}
    </div>
  );
}
