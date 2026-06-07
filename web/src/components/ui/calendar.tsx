import * as React from "react";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { DayPicker } from "react-day-picker";
import type { DayPickerProps } from "react-day-picker";

import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";

export function Calendar({ className, classNames, showOutsideDays = true, ...props }: DayPickerProps) {
  return (
    <DayPicker
      showOutsideDays={showOutsideDays}
      className={cn("p-0", className)}
      classNames={{
        months: "flex flex-col gap-4 sm:flex-row",
        month: "space-y-3",
        month_caption: "flex h-8 items-center justify-center",
        caption_label: "text-sm font-medium text-gray-1000",
        nav: "absolute right-3 top-3 flex items-center gap-1",
        button_previous: cn(buttonVariants({ variant: "secondary", size: "tiny" }), "h-7 w-7 p-0"),
        button_next: cn(buttonVariants({ variant: "secondary", size: "tiny" }), "h-7 w-7 p-0"),
        month_grid: "w-full border-collapse space-y-1",
        weekdays: "flex",
        weekday: "w-9 rounded-md text-[0.8rem] font-normal text-gray-700",
        week: "mt-2 flex w-full",
        day: "relative h-9 w-9 p-0 text-center text-sm focus-within:relative focus-within:z-20",
        day_button: cn(
          buttonVariants({ variant: "tertiary", size: "tiny" }),
          "h-9 w-9 rounded-md p-0 font-normal aria-selected:opacity-100"
        ),
        range_start: "rounded-l-md bg-gray-alpha-200",
        range_middle: "bg-gray-alpha-200",
        range_end: "rounded-r-md bg-gray-alpha-200",
        selected: "[&>button]:bg-gray-1000 [&>button]:text-background-100 [&>button]:hover:bg-gray-900",
        today: "[&>button]:border [&>button]:border-blue-700",
        outside: "text-gray-600 opacity-50",
        disabled: "text-gray-500 opacity-50",
        hidden: "invisible",
        ...classNames
      }}
      components={{
        Chevron: ({ orientation }) => (
          orientation === "left" ? <ChevronLeft size={14} /> : <ChevronRight size={14} />
        )
      }}
      {...props}
    />
  );
}
