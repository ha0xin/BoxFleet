import type { ReactNode } from "react";
import { Breadcrumbs, Sidebar } from "@cloudflare/kumo";

import { adminBasename } from "@/navigation";
import { usePublishStatus } from "@/publish/publish-status";
import { PublishStrip, publishBarToneClass } from "@/publish/publish-strip";

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
  const { status } = usePublishStatus();
  return (
    <div className="flex flex-col">
      <div
        className={`flex min-h-[58px] shrink-0 flex-wrap items-center justify-between gap-2 border-b border-kumo-line px-4 py-2 transition-colors duration-300 sm:px-6 ${publishBarToneClass(status)}`}
      >
        <div className="flex min-w-0 items-center gap-2">
          <Sidebar.Trigger className="md:hidden" />
          <Breadcrumbs size="sm">
            <Breadcrumbs.Link href={`${adminBasename()}/`}>BoxFleet</Breadcrumbs.Link>
            <Breadcrumbs.Separator />
            <Breadcrumbs.Current>{title}</Breadcrumbs.Current>
          </Breadcrumbs>
        </div>
        <div className="flex min-w-0 flex-wrap items-center justify-end gap-2 sm:gap-3">
          <PublishStrip />
          {actions ? <div className="flex shrink-0 items-center gap-2">{actions}</div> : null}
        </div>
      </div>

      <div className="mx-auto flex w-full max-w-[1400px] flex-col gap-6 px-6 py-6 md:px-8 lg:px-10">
        <div className="flex flex-col gap-2">
          <h1 className="text-xl font-semibold tracking-tight text-kumo-default md:text-3xl">{title}</h1>
          {description ? <p className="max-w-2xl text-base leading-5 text-kumo-subtle lg:text-lg">{description}</p> : null}
        </div>
        {children}
      </div>
    </div>
  );
}
