import type { ReactNode } from "react";
import { Breadcrumbs } from "@cloudflare/kumo";

import { adminBasename } from "@/navigation";

/**
 * App page header. The top breadcrumb bar is exactly `h-[58px]` with a bottom
 * hairline so it lines up with Kumo's `Sidebar.Header` (also `h-[58px]`,
 * `border-b border-kumo-line`) — the two borders form one continuous line.
 *
 * The bar's right slot (`actions`) is where page-level controls live (e.g. the
 * future "review & publish" button). Title + description + page `children` sit
 * in the padded content area below.
 */
export function AppPageHeader({
  title,
  description,
  actions,
  children
}: {
  title: string;
  description?: string;
  actions?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <div className="flex flex-col">
      <div className="flex h-[58px] shrink-0 items-center justify-between gap-4 border-b border-kumo-line px-6">
        <Breadcrumbs size="sm">
          <Breadcrumbs.Link href={`${adminBasename()}/`}>BoxFleet</Breadcrumbs.Link>
          <Breadcrumbs.Separator />
          <Breadcrumbs.Current>{title}</Breadcrumbs.Current>
        </Breadcrumbs>
        {actions ? <div className="flex shrink-0 items-center gap-2">{actions}</div> : null}
      </div>

      <div className="flex flex-col gap-6 px-6 py-6">
        <div className="flex flex-col gap-2">
          <h1 className="text-3xl font-semibold tracking-tight text-kumo-default">{title}</h1>
          {description ? <p className="max-w-prose text-base text-kumo-subtle">{description}</p> : null}
        </div>
        {children}
      </div>
    </div>
  );
}
