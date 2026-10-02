import { Button, Code, Table } from "@cloudflare/kumo";
import { CaretDownIcon, CaretRightIcon } from "@phosphor-icons/react";

export function LogExpandButton({ expanded, onClick, label }: { expanded: boolean; onClick: () => void; label: string }) {
  return <Button variant="ghost" size="sm" shape="square" icon={expanded ? CaretDownIcon : CaretRightIcon}
    aria-label={`${expanded ? "Collapse" : "Expand"} ${label}`} aria-expanded={expanded} onClick={onClick} />;
}

export function LogRowDetails({ value, colSpan }: { value: unknown; colSpan: number }) {
  return <Table.Row className="bf-log-detail"><Table.Cell colSpan={colSpan}>
    <div className="max-w-full overflow-x-auto py-2" aria-label="Log entry details">
      <Code.Block code={JSON.stringify(value, null, 2)} lang="jsonc" />
    </div>
  </Table.Cell></Table.Row>;
}
