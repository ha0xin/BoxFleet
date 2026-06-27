import { useMemo } from "react";
import { diffLines } from "diff";
import { ArrowRightIcon } from "@phosphor-icons/react";
import { Button, Dialog } from "@cloudflare/kumo";

import type { ConfigChange } from "@/types";
import { usePublishStatus } from "./publish-status";

type DiffRow = { text: string; kind: "added" | "removed" | "context" };

function buildRows(before: string, after: string): DiffRow[] {
  const rows: DiffRow[] = [];
  for (const part of diffLines(before, after)) {
    const kind: DiffRow["kind"] = part.added ? "added" : part.removed ? "removed" : "context";
    // diffLines keeps trailing newlines; drop the empty tail so we don't render
    // a blank final row per hunk.
    const lines = part.value.replace(/\n$/, "").split("\n");
    for (const text of lines) rows.push({ text, kind });
  }
  return rows;
}

function rowClass(kind: DiffRow["kind"]): string {
  switch (kind) {
    case "added":
      return "bg-kumo-success-tint text-kumo-success";
    case "removed":
      return "bg-kumo-danger-tint text-kumo-danger";
    default:
      return "text-kumo-subtle";
  }
}

function rowSign(kind: DiffRow["kind"]): string {
  return kind === "added" ? "+" : kind === "removed" ? "-" : " ";
}

function NodeDiff({ change }: { change: ConfigChange }) {
  const rows = useMemo(
    () => buildRows(change.target_config, change.rendered_config),
    [change.target_config, change.rendered_config]
  );
  return (
    <div className="overflow-hidden rounded-lg border border-kumo-line">
      <div className="flex items-center gap-2 border-b border-kumo-line bg-kumo-canvas px-3 py-2 text-sm">
        <span className="font-semibold text-kumo-default">{change.node}</span>
        <span className="flex items-center gap-1 text-kumo-subtle">
          {change.target_version || "unpublished"}
          <ArrowRightIcon className="size-3.5" />
          new version
        </span>
      </div>
      <div className="max-h-[40vh] overflow-auto font-mono text-xs leading-5">
        {rows.map((row, index) => (
          <div key={index} className={`flex gap-2 px-3 ${rowClass(row.kind)}`}>
            <span className="select-none opacity-60">{rowSign(row.kind)}</span>
            <span className="whitespace-pre-wrap break-all">{row.text || " "}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

export function PublishDiffDialog() {
  const { isDiffOpen, closeDiff, publish, changes, changedCount } = usePublishStatus();

  return (
    <Dialog.Root open={isDiffOpen} onOpenChange={(open) => (open ? undefined : closeDiff())}>
      <Dialog size="xl" className="p-6">
        <div className="mb-1 flex items-start justify-between gap-4">
          <Dialog.Title className="text-xl font-semibold text-kumo-default">
            Review configuration changes
          </Dialog.Title>
        </div>
        <Dialog.Description className="mb-4 text-kumo-subtle">
          {changedCount} {changedCount === 1 ? "node" : "nodes"} will be published. Agents apply the
          new config on their next pull cycle (up to ~1 minute).
        </Dialog.Description>

        <div className="flex max-h-[55vh] flex-col gap-4 overflow-y-auto">
          {changes.map((change) => (
            <NodeDiff key={change.node} change={change} />
          ))}
          {changes.length === 0 ? (
            <p className="text-kumo-subtle">No pending changes.</p>
          ) : null}
        </div>

        <div className="mt-6 flex justify-end gap-2">
          <Dialog.Close
            render={(props) => (
              <Button {...props} variant="ghost">
                Cancel
              </Button>
            )}
          />
          <Button onClick={publish} disabled={changes.length === 0}>
            Publish &amp; apply
          </Button>
        </div>
      </Dialog>
    </Dialog.Root>
  );
}
