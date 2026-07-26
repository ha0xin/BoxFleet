import { ArrowsClockwiseIcon, CheckIcon, SignOutIcon } from "@phosphor-icons/react";
import { Button, Grid, SensitiveInput, Surface, Text } from "@cloudflare/kumo";
import { useIsFetching } from "@tanstack/react-query";

import { adminKeys } from "@/admin/query";
import { AppPageHeader } from "@/components/app-page-header";

export function SettingsPage({
  tokenInput,
  setTokenInput,
  activeToken,
  applyToken,
  logout,
  refresh
}: {
  tokenInput: string;
  setTokenInput: (value: string) => void;
  activeToken: string;
  applyToken: () => void;
  logout: () => void;
  refresh: () => void;
}) {
  const refreshing = useIsFetching({ queryKey: adminKeys.root }) > 0;
  const unchanged = tokenInput.trim() === activeToken.trim();

  return (
    <div className="flex min-h-full flex-col bg-kumo-canvas">
      <AppPageHeader title="Settings" description="Admin authentication and data." />
      <main className="w-full grow bg-kumo-canvas">
        <div className="mx-auto w-full max-w-[1400px] px-6 pb-8 md:px-8 lg:px-10">
          <Grid variant="2up" gap="base">
            <Surface id="admin-token" className="rounded-lg p-5 scroll-mt-4">
              <Text variant="heading3" as="h2">
                Admin token
              </Text>
              <div className="mt-1">
                <Text variant="secondary" size="sm">
                  Stored in this browser and sent as a bearer token on every admin request.
                </Text>
              </div>
              <form
                className="mt-4 flex flex-col gap-3"
                onSubmit={(event) => {
                  event.preventDefault();
                  applyToken();
                }}
              >
                <SensitiveInput
                  id="admin-token-input"
                  size="sm"
                  aria-label="Admin token"
                  placeholder="Admin token"
                  value={tokenInput}
                  onChange={(event) => setTokenInput(event.target.value)}
                />
                <div className="flex items-center gap-2">
                  <Button type="submit" variant="primary" icon={CheckIcon} disabled={unchanged}>
                    Apply
                  </Button>
                  {activeToken ? (
                    <Button type="button" variant="secondary-destructive" icon={SignOutIcon} onClick={logout}>
                      Sign out
                    </Button>
                  ) : null}
                </div>
              </form>
            </Surface>

            <Surface className="rounded-lg p-5">
              <Text variant="heading3" as="h2">
                Data
              </Text>
              <div className="mt-1">
                <Text variant="secondary" size="sm">
                  Reload nodes, users, traffic, and logs from the server.
                </Text>
              </div>
              <div className="mt-4">
                <Button
                  variant="secondary"
                  icon={ArrowsClockwiseIcon}
                  loading={refreshing}
                  onClick={() => refresh()}
                >
                  Refresh data
                </Button>
              </div>
            </Surface>
          </Grid>
        </div>
      </main>
    </div>
  );
}
