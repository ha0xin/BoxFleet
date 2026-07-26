import type { ReactNode } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { FileTextIcon } from "@phosphor-icons/react";
import { Breadcrumbs, LinkButton, Sidebar } from "@cloudflare/kumo";

import { adminBasename } from "@/navigation";
import { usePublishStatus } from "@/publish/publish-status";
import { PublishStrip, publishBarToneClass } from "@/publish/publish-strip";

/**
 * The single app page header: a breadcrumb top bar aligned with Kumo's
 * `Sidebar.Header` (both `min-h-[58px]` with a bottom hairline, so the two
 * borders read as one continuous line) followed by the page title block.
 *
 * The bar's right slot carries the publish strip, a Logs shortcut (hidden on
 * the System Logs page itself), and the page-level `actions`. Every admin page
 * renders this once at the top; page content below owns its own
 * `max-w-[1400px]` container.
 */
export function AppPageHeader({
  title,
  description,
  actions
}: {
  title: string;
  description?: string;
  actions?: ReactNode;
}) {
  const { status } = usePublishStatus();
  const navigate = useNavigate();
  const location = useLocation();
  const onSystemLogs = location.pathname.startsWith("/system-logs");

  return (
    <div className="flex flex-col">
      <div
        className={`flex min-h-[58px] shrink-0 flex-wrap items-center justify-between gap-2 border-b border-kumo-line px-4 py-2 transition-colors duration-300 sm:px-6 ${publishBarToneClass(status)}`}
      >
        <div className="flex min-w-0 items-center gap-2">
          <Sidebar.Trigger className="md:hidden" />
          <Breadcrumbs size="sm">
            <span
              onClickCapture={(event) => {
                if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
                event.preventDefault();
                navigate("/");
              }}
            >
              <Breadcrumbs.Link href={`${adminBasename()}/`}>BoxFleet</Breadcrumbs.Link>
            </span>
            <Breadcrumbs.Separator />
            <Breadcrumbs.Current>{title}</Breadcrumbs.Current>
          </Breadcrumbs>
        </div>
        <div className="ml-auto flex min-w-0 flex-wrap items-center justify-end gap-2 sm:gap-3">
          <PublishStrip />
          {!onSystemLogs ? (
            <LinkButton
              variant="ghost"
              size="sm"
              icon={FileTextIcon}
              href={`${adminBasename()}/system-logs`}
              onClick={(event) => {
                if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
                event.preventDefault();
                navigate("/system-logs");
              }}
            >
              <span className="hidden md:inline">Logs</span>
            </LinkButton>
          ) : null}
        </div>
      </div>

      <div className="mx-auto w-full max-w-[1400px] px-6 md:px-8 lg:px-10">
        <header className="mb-4 flex flex-wrap items-start justify-between gap-4 pt-6">
          <div className="flex min-w-0 flex-col">
            <h1 className="mb-1.5 text-xl font-semibold tracking-tight text-kumo-default md:text-3xl">{title}</h1>
            {description ? (
              <p className="max-w-2xl text-base leading-5 text-kumo-subtle lg:text-lg">{description}</p>
            ) : null}
          </div>
          {actions ? <div className="flex shrink-0 items-center gap-2">{actions}</div> : null}
        </header>
      </div>
    </div>
  );
}
