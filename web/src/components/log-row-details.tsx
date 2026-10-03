import { Button, ClipboardText, Code, Table } from "@cloudflare/kumo";
import { CaretRightIcon } from "@phosphor-icons/react";

export function LogExpandButton({ expanded, onClick, label }: { expanded: boolean; onClick: () => void; label: string }) {
  return <Button variant="ghost" size="xs" shape="square" className="bf-log-expand"
    aria-label={`${expanded ? "Collapse" : "Expand"} ${label}`} aria-expanded={expanded} onClick={onClick}><CaretRightIcon size={16} className={`text-kumo-subtle transition-transform duration-150 ${expanded ? "rotate-90" : ""}`} /></Button>;
}

export function LogRowDetails({ value, colSpan }: { value: unknown; colSpan: number }) {
  return <Table.Row className="bf-log-detail"><Table.Cell colSpan={colSpan}>
    <div className="max-w-full overflow-x-auto py-2" aria-label="Log entry details">
      <div className="mb-2 flex justify-end"><ClipboardText size="sm" text="Copy JSON" textToCopy={JSON.stringify(value, null, 2)} className="max-w-40" /></div>
      <Code.Block code={JSON.stringify(value, null, 2)} lang="jsonc" />
    </div>
  </Table.Cell></Table.Row>;
}
