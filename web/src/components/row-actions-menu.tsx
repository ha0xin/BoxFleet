import type { ReactNode } from "react";
import { DotsThreeIcon } from "@phosphor-icons/react";
import { Button, DropdownMenu } from "@cloudflare/kumo";

/**
 * Canonical per-row actions menu: a three-dot kebab trigger opening a
 * DropdownMenu. `label` names the row for assistive tech ("Actions for X").
 * Children are `DropdownMenu.Item` / `DropdownMenu.Separator` elements.
 */
export function RowActionsMenu({ label, children }: { label: string; children: ReactNode }) {
  return (
    <DropdownMenu>
      <DropdownMenu.Trigger
        render={
          <Button variant="ghost" size="sm" shape="square" aria-label={label}>
            <DotsThreeIcon className="size-4" />
          </Button>
        }
      />
      <DropdownMenu.Content>{children}</DropdownMenu.Content>
    </DropdownMenu>
  );
}
