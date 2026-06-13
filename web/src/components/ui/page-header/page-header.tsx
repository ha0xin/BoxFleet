import { ReactNode } from "react";
import { Tabs, cn, type TabsItem } from "@cloudflare/kumo";

export const KUMO_PAGE_HEADER_VARIANTS = {
  spacing: {
    compact: {
      classes: "gap-1",
      description: "Compact spacing between header elements",
    },
    base: {
      classes: "gap-2",
      description: "Default spacing between header elements",
    },
    relaxed: {
      classes: "gap-4",
      description: "Relaxed spacing for more prominent headers",
    },
  },
} as const;

export const KUMO_PAGE_HEADER_DEFAULT_VARIANTS = {
  spacing: "base",
} as const;

export type KumoPageHeaderSpacing =
  keyof typeof KUMO_PAGE_HEADER_VARIANTS.spacing;

export interface KumoPageHeaderVariantsProps {
  spacing?: KumoPageHeaderSpacing;
}

export function pageHeaderVariants({
  spacing = KUMO_PAGE_HEADER_DEFAULT_VARIANTS.spacing,
}: KumoPageHeaderVariantsProps = {}) {
  return cn(
    "flex flex-col",
    KUMO_PAGE_HEADER_VARIANTS.spacing[spacing].classes,
  );
}

export interface PageHeaderProps extends KumoPageHeaderVariantsProps {
  /** Optional breadcrumb trail. BoxFleet nav is flat, so this is usually omitted. */
  breadcrumbs?: ReactNode;
  title?: string;
  description?: string;
  tabs?: TabsItem[];
  defaultTab?: string;
  onValueChange?: (value: string) => void;
  /** Page-level action buttons, rendered top-right next to the title. */
  children?: React.ReactNode;
  className?: string;
}

export function PageHeader({
  breadcrumbs,
  title,
  description,
  tabs,
  defaultTab,
  onValueChange,
  spacing = "base",
  className,
  children,
}: PageHeaderProps) {
  return (
    <div className={cn(pageHeaderVariants({ spacing }), className)}>
      {breadcrumbs && (
        <div className="border-b border-kumo-line">{breadcrumbs}</div>
      )}

      {(title || description || children) && (
        <div className="flex w-full items-start justify-between gap-4 py-3">
          {(title || description) && (
            <div className="flex flex-col gap-1">
              {title && (
                <h1 className="font-heading text-2xl font-semibold tracking-tight text-kumo-default">
                  {title}
                </h1>
              )}
              {description && (
                <p className="max-w-prose text-sm text-kumo-subtle">
                  {description}
                </p>
              )}
            </div>
          )}
          {children && (
            <div className="flex shrink-0 items-center gap-2">{children}</div>
          )}
        </div>
      )}

      {tabs && (
        <div className="flex w-full items-center justify-between border-b border-kumo-line pt-1 pb-3">
          <Tabs
            tabs={tabs}
            selectedValue={defaultTab}
            onValueChange={(nextValue) => {
              const stringValue = String(nextValue);
              onValueChange?.(stringValue);
            }}
          />
        </div>
      )}
    </div>
  );
}
