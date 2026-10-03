import type { ReactNode } from "react";
import { useNavigate } from "react-router-dom";
import { ListIcon, MoonIcon, SunIcon } from "@phosphor-icons/react";
import { Breadcrumbs, Button, Sidebar, useSidebar } from "@cloudflare/kumo";

import { adminBasename } from "@/navigation";
import { usePublishStatus } from "@/publish/publish-status";
import { PublishStrip, publishBarToneClass } from "@/publish/publish-strip";
import { toggleColorMode } from "@/components/color-mode";
import { useIsDarkMode } from "@/components/chart/use-color-mode";

/**
 * The single app page header: a breadcrumb top bar aligned with Kumo's
 * `Sidebar.Header` (both `min-h-[58px]` with a bottom hairline, so the two
 * borders read as one continuous line) followed by the page title block.
 *
 * The bar's right slot carries compact page actions and the color mode toggle. Publication notices occupy a separate row so pending
 * changes cannot stretch the navigation bar. Every admin page
 * renders this once at the top; page content below owns its own
 * responsive content container.
 */
export function AppPageHeader({
  title,
  description,
  actions,
  compact = false
}: {
  title: string;
  description?: string;
  actions?: ReactNode;
  compact?: boolean;
}) {
  const { status, changesError } = usePublishStatus();
  const navigate = useNavigate();
  const { openMobile } = useSidebar();
  const isDarkMode = useIsDarkMode();

  return (
    <div className="flex flex-col">
      <div
        className={`bf-page-topbar flex min-h-[58px] shrink-0 flex-wrap items-center justify-between gap-2 border-b border-kumo-line px-4 py-2 transition-colors duration-300 sm:px-6`}
      >
        <div className="flex min-w-0 items-center gap-2">
          <Sidebar.Trigger className="lg:hidden" aria-label="Toggle sidebar navigation" aria-expanded={openMobile}><ListIcon size={20} /></Sidebar.Trigger>
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
          {compact ? actions : null}
          <Button
            type="button"
            variant="ghost"
            size="sm"
            shape="square"
            icon={isDarkMode ? SunIcon : MoonIcon}
            aria-label={isDarkMode ? "Switch to light mode" : "Switch to dark mode"}
            title={isDarkMode ? "Light mode" : "Dark mode"}
            onClick={toggleColorMode}
          />
        </div>
      </div>

      {status !== "idle" || changesError ? <div className={`bf-page-publish transition-colors duration-300 border-b border-kumo-line px-4 py-2 sm:px-6 ${publishBarToneClass(status)}`}><PublishStrip /></div> : null}

      {compact ? <h1 className="sr-only">{title}</h1> : <div className="mx-auto w-full px-4 md:px-8">
        <header className="mb-6 flex flex-wrap items-start justify-between gap-4 border-b border-kumo-line py-8">
          <div className="flex min-w-0 flex-col">
            <h1 className="mb-2 text-xl font-semibold tracking-tight text-kumo-default">{title}</h1>
            {description ? (
              <p className="max-w-2xl text-sm leading-5 text-kumo-subtle">{description}</p>
            ) : null}
          </div>
          {actions ? <div className="flex shrink-0 items-center gap-2">{actions}</div> : null}
        </header>
      </div>}
    </div>
  );
}
