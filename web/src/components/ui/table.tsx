import * as React from "react";
import { Table as KumoTable } from "@cloudflare/kumo/components/table";

import { cn } from "@/lib/utils";

export const Table = React.forwardRef<HTMLTableElement, React.ComponentPropsWithoutRef<typeof KumoTable>>(
  ({ className, ...props }, ref) => (
    <div className="relative w-full overflow-auto">
      <KumoTable ref={ref} className={cn("text-sm", className)} {...props} />
    </div>
  )
);
Table.displayName = "Table";

export const TableHeader = KumoTable.Header;
export const TableBody = KumoTable.Body;
export const TableRow = KumoTable.Row;
export const TableHead = KumoTable.Head;
export const TableCell = KumoTable.Cell;

export const TableCaption = React.forwardRef<HTMLTableCaptionElement, React.HTMLAttributes<HTMLTableCaptionElement>>(
  ({ className, ...props }, ref) => (
    <caption ref={ref} className={cn("mt-4 text-sm text-kumo-subtle", className)} {...props} />
  )
);
TableCaption.displayName = "TableCaption";
