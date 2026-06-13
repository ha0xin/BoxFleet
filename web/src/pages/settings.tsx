import { ArrowsClockwise, Check, SignOut } from "@phosphor-icons/react";
import { Button, Grid, SensitiveInput, Surface, Text } from "@cloudflare/kumo";

import { AppPageHeader } from "@/components/app-page-header";

export function SettingsPage({
  tokenInput,
  setTokenInput,
  activeToken,
  applyToken,
  logout,
  refresh,
  refreshing
}: {
  tokenInput: string;
  setTokenInput: (value: string) => void;
  activeToken: string;
  applyToken: () => void;
  logout: () => void;
  refresh: () => void;
  refreshing: boolean;
}) {
  const unchanged = tokenInput.trim() === activeToken.trim();

  return (
    <AppPageHeader title="Settings" description="Admin authentication and data.">
      <Grid variant="2up" gap="base">
        <Surface className="rounded-lg p-5">
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
              size="sm"
              placeholder="Admin token"
              value={tokenInput}
              onChange={(event) => setTokenInput(event.target.value)}
            />
            <div className="flex items-center gap-2">
              <Button type="submit" variant="primary" icon={<Check />} disabled={unchanged}>
                Apply
              </Button>
              {activeToken ? (
                <Button type="button" variant="secondary-destructive" icon={<SignOut />} onClick={logout}>
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
              icon={<ArrowsClockwise />}
              loading={refreshing}
              onClick={() => refresh()}
            >
              Refresh data
            </Button>
          </div>
        </Surface>
      </Grid>
    </AppPageHeader>
  );
}
