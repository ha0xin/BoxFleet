import * as React from "react";
import { X } from "lucide-react";
import {
  Dialog as KumoDialog,
  type DialogProps as KumoDialogProps,
  type DialogRootProps
} from "@cloudflare/kumo/components/dialog";

import { cn } from "@/lib/utils";
import { Button } from "./button";

export const Dialog = KumoDialog.Root;
export const DialogTrigger = KumoDialog.Trigger;
export const DialogClose = KumoDialog.Close;
export const DialogPortal = ({ children }: { children?: React.ReactNode }) => <>{children}</>;
export const DialogOverlay = () => null;

type DialogSize = "md" | "lg" | "xl";

const sizeMap: Record<DialogSize, KumoDialogProps["size"]> = {
  md: "base",
  lg: "lg",
  xl: "xl"
};

export const DialogContent = React.forwardRef<HTMLDivElement, Omit<KumoDialogProps, "size"> & { size?: DialogSize }>(
  ({ className, children, size = "md", ...props }, ref) => (
    <div ref={ref}>
      <KumoDialog
        size={sizeMap[size]}
        className={cn("max-h-[90vh] overflow-y-auto overflow-x-hidden p-6 [&>*]:min-w-0", className)}
        {...props}
      >
        {children}
        <KumoDialog.Close
          render={
            <Button
              type="button"
              aria-label="Close"
              shape="square"
              svgOnly
              size="tiny"
              variant="tertiary"
              className="absolute right-4 top-4"
            >
              <X className="h-4 w-4" />
            </Button>
          }
        />
      </KumoDialog>
    </div>
  )
);
DialogContent.displayName = "DialogContent";

export function DialogHeader({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("flex flex-col space-y-1.5 pr-8", className)} {...props} />;
}

export function DialogFooter({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn("flex flex-col-reverse gap-2 sm:flex-row sm:items-center sm:justify-end", className)}
      {...props}
    />
  );
}

export const DialogTitle = KumoDialog.Title;
export const DialogDescription = KumoDialog.Description;

export type { DialogRootProps };
