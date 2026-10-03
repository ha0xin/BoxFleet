import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";
import { Button, Checkbox, Input } from "@cloudflare/kumo";
import { SidebarSimpleIcon, XIcon } from "@phosphor-icons/react";

export type LogField = { id: string; label: string; visible: boolean; toggle: (visible: boolean) => void };
type FieldCatalog = { fields: LogField[]; reset: () => void };
const LogFieldsContext = createContext<{ open: boolean; toggle: () => void; register: (catalog: FieldCatalog | null) => void } | null>(null);
export const useLogFields = () => useContext(LogFieldsContext);

/** One log workbench for structured connection events and service journals. */
export function LogWorkspace({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState(false);
  const [catalog, setCatalog] = useState<FieldCatalog | null>(null);
  const [search, setSearch] = useState("");
  const register = useCallback((value: FieldCatalog | null) => setCatalog(value), []);
  const toggle = useCallback(() => setOpen((value) => !value), []);
  const context = useMemo(() => ({ open, toggle, register }), [open, toggle, register]);
  return <LogFieldsContext.Provider value={context}>
    <div className={`bf-log-workspace ${open ? "bf-fields-open" : ""}`}>
      {open ? <aside className="bf-field-catalog bg-kumo-base" aria-label="Log fields">
        <div className="flex items-center gap-2 border-b border-kumo-line p-3">
          <Input aria-label="Search fields" placeholder="Search fields…" value={search} onChange={(event) => setSearch(event.target.value)} className="min-w-0 flex-1" />
          <Button variant="ghost" shape="square" size="sm" icon={XIcon} aria-label="Close fields" onClick={toggle} />
        </div>
        <div className="flex flex-col gap-3 p-4">
          <span className="text-xs font-medium text-kumo-subtle">Event fields</span>
          {catalog?.fields.filter((field) => field.label.toLowerCase().includes(search.toLowerCase())).map((field) => <Checkbox key={field.id} label={field.label} checked={field.visible} onCheckedChange={(visible) => { setCatalog((previous) => previous ? { ...previous, fields: previous.fields.map((item) => item.id === field.id ? { ...item, visible } : item) } : null); field.toggle(visible); }} />)}
          {catalog ? <Button variant="ghost" size="sm" onClick={catalog.reset}>Reset columns</Button> : null}
        </div>
      </aside> : null}
      <div className="bf-log-workspace-content">{children}</div>
    </div>
  </LogFieldsContext.Provider>;
}

export function LogFieldsToggle() {
  const fields = useLogFields();
  return fields ? <Button variant="secondary" shape="square" icon={SidebarSimpleIcon} aria-label="Toggle fields sidebar" aria-expanded={fields.open} onClick={fields.toggle} /> : null;
}

export function logTimezone() {
  const minutes = -new Date().getTimezoneOffset();
  return `GMT${minutes < 0 ? "−" : "+"}${Math.floor(Math.abs(minutes) / 60)}${Math.abs(minutes) % 60 ? `:${String(Math.abs(minutes) % 60).padStart(2, "0")}` : ""}`;
}
