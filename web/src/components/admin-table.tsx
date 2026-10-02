import { Children, cloneElement, isValidElement, useEffect, useId, useMemo, useRef, useState, type ReactElement, type ReactNode } from "react";
import { LogRowDetails } from "./log-row-details";
import { getCoreRowModel, useReactTable, type ColumnSizingState, type VisibilityState } from "@tanstack/react-table";
import { ArrowsCounterClockwiseIcon, GearSixIcon, SlidersHorizontalIcon, ArrowDownIcon, CaretUpDownIcon, WarningCircleIcon } from "@phosphor-icons/react";
import { Button, DropdownMenu, Empty, Loader, Pagination, Table } from "@cloudflare/kumo";

export type SortDirection = "asc" | "desc";

type TableElement = ReactElement<{ children?: ReactNode; label?: string; widths?: readonly TableColumnWidth[]; className?: string; colSpan?: number; style?: React.CSSProperties; resizeHandle?: ReactNode; "aria-label"?: string }>;
const emptyRows: unknown[] = [];

function elements(children: ReactNode): TableElement[] {
  return Children.toArray(children).filter(isValidElement) as TableElement[];
}

function textOf(node: ReactNode): string {
  return Children.toArray(node).map((child) => isValidElement(child)
    ? textOf((child.props as { children?: ReactNode }).children)
    : typeof child === "string" || typeof child === "number" ? String(child) : "").join("");
}

function findHeaders(node: ReactNode): TableElement[] {
  for (const child of elements(node)) {
    if (child.type === Table.Header) return elements(child.props.children).flatMap((row) => elements(row.props.children));
    const found = findHeaders(child.props.children);
    if (found.length) return found;
  }
  return [];
}

function findWidths(node: ReactNode): readonly TableColumnWidth[] | undefined {
  for (const child of elements(node)) {
    if (child.type === TableColgroup) return child.props.widths;
    const found = findWidths(child.props.children);
    if (found) return found;
  }
}

/** Stored preferences are local to a table and its column schema, never its data. */
export function readTablePreferences(key: string, count: number): { sizing: ColumnSizingState; visibility: VisibilityState } {
  try {
    const value = JSON.parse(localStorage.getItem(key) ?? "{}");
    const sizing: ColumnSizingState = {};
    const visibility: VisibilityState = {};
    for (let index = 0; index < count; index++) {
      const width = value?.sizing?.[index];
      if (typeof width === "number" && Number.isFinite(width)) sizing[index] = Math.max(64, Math.min(1200, width));
      if (index > 0 && value?.visibility?.[index] === false) visibility[index] = false;
    }
    return { sizing, visibility };
  } catch {
    return { sizing: {}, visibility: {} };
  }
}

/**
 * Native Kumo table presentation with TanStack's column sizing/visibility.
 * Pages still own their rows, query state and server pagination. No DOM mutation
 * or second data model is needed to give every inventory the same table tools.
 */
export function TableCard({ children, className = "", tableId, widths, variant = "resource" }: {
  children: ReactNode;
  className?: string;
  tableId?: string;
  widths?: readonly TableColumnWidth[];
  variant?: "resource" | "log";
}) {
  const handleId = useId();
  const headers = findHeaders(children);
  const labels = headers.map((head, index) => head.props.label ?? (textOf(head.props.children).trim() || `Column ${index + 1}`));
  const declared = widths ?? findWidths(children) ?? labels.map((label, index) => label === "Actions" ? 52 : index === 0 ? { min: 220 } : 160);
  const schema = JSON.stringify({ labels, declared });
  const storageKey = `boxfleet.table.v1.${tableId ?? "anonymous"}.${schema}`;
  const [preferences, setPreferences] = useState(() => tableId ? readTablePreferences(storageKey, labels.length) : { sizing: {}, visibility: {} });
  const scrollRef = useRef<HTMLDivElement>(null);
  const [availableWidth, setAvailableWidth] = useState(0);
  useEffect(() => {
    const element = scrollRef.current;
    if (!element || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => setAvailableWidth(element.clientWidth));
    observer.observe(element);
    return () => observer.disconnect();
  }, []);
  useEffect(() => {
    if (!tableId) return;
    try { localStorage.setItem(storageKey, JSON.stringify(preferences)); } catch { /* Storage can be disabled. */ }
  }, [storageKey, tableId, preferences]);

  const definitions = useMemo(() => {
    const spec = JSON.parse(schema) as { labels: string[]; declared: TableColumnWidth[] };
    const sizes = spec.declared.map((width) => typeof width === "number" ? width : width.min);
    // Spare space belongs to the widest flexible content column, rather than
    // making every name column equally wide. Manual widths override this default.
    const flexible = spec.declared.reduce<number>((best, width, index) => typeof width !== "number" && (best < 0 || sizes[index] > sizes[best]) ? index : best, -1);
    if (flexible >= 0) sizes[flexible] += Math.max(0, availableWidth - 44 - sizes.reduce((sum, size) => sum + size, 0));
    return spec.labels.map((label, index) => ({ id: String(index), header: label, size: sizes[index], minSize: sizes[index] < 64 ? sizes[index] : 64, maxSize: 1200, enableResizing: label !== "Actions" && label !== "Details", enableHiding: index > 0 && label !== "Actions" && label !== "Details" }));
  }, [schema, availableWidth]);
  const table = useReactTable({
    data: emptyRows,
    columns: definitions,
    getCoreRowModel: getCoreRowModel(),
    columnResizeMode: "onChange",
    state: { columnSizing: preferences.sizing, columnVisibility: preferences.visibility },
    onColumnSizingChange: (update) => setPreferences((previous) => ({ ...previous, sizing: typeof update === "function" ? update(previous.sizing) : update })),
    onColumnVisibilityChange: (update) => setPreferences((previous) => ({ ...previous, visibility: typeof update === "function" ? update(previous.visibility) : update }))
  });
  const settings = (
    <DropdownMenu>
      <DropdownMenu.Trigger render={variant === "log" ? <Button variant="secondary" size="xs" icon={SlidersHorizontalIcon} aria-label="Fields">Fields</Button> : <Button variant="ghost" size="sm" shape="square" icon={GearSixIcon} aria-label="Edit columns" />} />
      <DropdownMenu.Content>
        <DropdownMenu.Group>
          <DropdownMenu.Label>Columns</DropdownMenu.Label>
          {table.getAllLeafColumns().filter((column) => column.getCanHide()).map((column) => (
            <DropdownMenu.CheckboxItem key={column.id} checked={column.getIsVisible()} closeOnClick={false} onCheckedChange={(visible) => column.toggleVisibility(visible)}>
              {labels[Number(column.id)]}
            </DropdownMenu.CheckboxItem>
          ))}
        </DropdownMenu.Group>
        <DropdownMenu.Separator />
        <DropdownMenu.Item icon={ArrowsCounterClockwiseIcon} onClick={() => setPreferences({ sizing: {}, visibility: {} })}>Reset columns</DropdownMenu.Item>
      </DropdownMenu.Content>
    </DropdownMenu>
  );
  function resizeHandle(index: number) {
    const column = table.getColumn(String(index));
    if (!column?.getCanResize()) return null;
    const header = table.getFlatHeaders().find((item) => item.column.id === column.id)!;
    return <><span id={`${handleId}-${index}`} className="sr-only" aria-hidden="true">{`Resize ${labels[index]} column`}</span><Table.ResizeHandle
      aria-labelledby={`${handleId}-${index}`}
      role="separator"
      aria-orientation="vertical"
      aria-valuemin={column.columnDef.minSize}
      aria-valuemax={1200}
      aria-valuenow={Math.round(column.getSize())}
      tabIndex={0}
      onMouseDown={header.getResizeHandler()}
      onTouchStart={header.getResizeHandler()}
      onDoubleClick={() => column.resetSize()}
      onKeyDown={(event) => {
        if (!["ArrowLeft", "ArrowRight", "Home"].includes(event.key)) return;
        event.preventDefault();
        if (event.key === "Home") column.resetSize();
        else table.setColumnSizing((current) => ({ ...current, [column.id]: Math.max(column.columnDef.minSize ?? 64, Math.min(1200, column.getSize() + (event.key === "ArrowRight" ? 1 : -1) * (event.shiftKey ? 40 : 10))) }));
      }}
    /></>;
  }
  const fillerWidth = Math.max(44, availableWidth - table.getTotalSize());
  const visibleCount = table.getVisibleLeafColumns().length + 1;
  function findFeedback(node: ReactNode): TableElement | undefined {
    for (const child of elements(node)) {
      if (child.type === TableEmpty || child.type === TableError || child.type === TableLoading) return child;
      const found = findFeedback(child.props.children);
      if (found) return found;
    }
  }
  const feedback = findFeedback(children);
  function decorate(node: ReactNode, inHeader = false): ReactNode {
    return Children.map(node, (child) => {
      if (!isValidElement(child)) return child;
      const element = child as TableElement;
      if (element.type === TableColgroup) return null;
      if (element.type === Table) return cloneElement(element, { style: { ...element.props.style, minWidth: undefined, width: table.getTotalSize() + fillerWidth }, className: `${element.props.className ?? ""} bf-data-table` },
        <colgroup>{table.getVisibleLeafColumns().map((column) => <col key={column.id} style={{ width: column.getSize() }} />)}<col style={{ width: fillerWidth }} /></colgroup>, decorate(element.props.children));
      if (element.type === Table.Header) return cloneElement(element, {}, decorate(element.props.children, true));
      if (feedback && element.type === feedback.type) return null;
      if (element.type === LogRowDetails) return cloneElement(element, { colSpan: visibleCount });
      if (element.type === Table.Row) {
        const cells = elements(element.props.children);
        if (cells.length === 1 && cells[0].props.colSpan) return cloneElement(element, {}, cloneElement(cells[0], { colSpan: visibleCount }));
        return cloneElement(element, {}, cells.map((cell, index) => {
          if (!table.getColumn(String(index))?.getIsVisible()) return null;
          if (inHeader && cell.type === SortHead) return cloneElement(cell, { resizeHandle: <>{variant === "log" && index === 1 ? <span className="ml-2 inline-flex align-middle">{settings}</span> : null}{resizeHandle(index)}</> });
          if (inHeader) return cloneElement(cell, { "aria-label": labels[index] }, cell.props.children, variant === "log" && index === 1 ? <span className="ml-2 inline-flex align-middle">{settings}</span> : null, resizeHandle(index));
          return cell;
        }), inHeader ? <Table.Head key="settings" sticky="right" className="bf-table-settings">{variant === "resource" ? settings : null}</Table.Head> : <Table.Cell key="settings" sticky="right" className="bf-table-settings" />);
      }
      if (element.props.children) return cloneElement(element, {}, decorate(element.props.children, inHeader));
      return element;
    });
  }
  return <div className={`bf-table-card bf-table-${variant} overflow-hidden rounded-lg border border-kumo-line bg-kumo-base ${className}`} data-table-id={tableId}>
    <div ref={scrollRef} className="bf-table-scroll overflow-x-auto overscroll-x-contain">{decorate(children)}</div>
    {feedback ? <Table className="w-full"><Table.Body>{cloneElement(feedback, { colSpan: 1 })}</Table.Body></Table> : null}
  </div>;
}

/**
 * Width of one column in a fixed-layout admin table.
 *
 * - A number is an exact px width. Use it for content with a known ceiling:
 *   status badges, version strings, counts, relative timestamps, the kebab.
 * - `{ min }` marks a flexible column that absorbs whatever width is left over.
 *   Use it for genuinely variable text — names, hosts, endpoints, log messages —
 *   and truncate inside the cell.
 *
 * Declaring widths is what stops the waste. Kumo's `<Table>` is `w-full`, so
 * under the default auto layout the browser smears every surplus pixel across
 * all columns in proportion to their content, which is why a one-character
 * "Config" cell used to be as wide as a hostname.
 *
 * Fixed table layout divides the leftover width **equally** between the flexible
 * columns — that is the one distribution CSS defines identically everywhere, so
 * it is what we rely on. A column that must stay narrower than its peers gets a
 * px width instead of being made flexible.
 */
export type TableColumnWidth = number | { min: number };

/**
 * Narrowest width the table can be laid out at. Because the leftover is split
 * equally, honouring every floor means reserving the *largest* flexible floor
 * for each flexible column, not the sum of the individual ones.
 *
 * Set the result as the table's `min-width` so `TableCard`'s scroll container —
 * not the page — takes over below it.
 */
export function tableMinWidth(widths: readonly TableColumnWidth[]): number {
  let fixed = 0;
  let flexible = 0;
  let largestFloor = 0;
  for (const width of widths) {
    if (typeof width === "number") {
      fixed += width;
    } else {
      flexible += 1;
      largestFloor = Math.max(largestFloor, width.min);
    }
  }
  return fixed + flexible * largestFloor;
}

/**
 * `<colgroup>` for a `<Table layout="fixed">`. Fixed columns get an exact width;
 * flexible columns are left `auto` so the browser divides the leftover width
 * between them.
 *
 * Must be the table's first child, before `<Table.Header>`. This is also the
 * declaration consumed by TableCard, which replaces it with TanStack sizes.
 * Outside TableCard it remains a standard fixed-layout colgroup.
 */
export function TableColgroup({ widths }: { widths: readonly TableColumnWidth[] }) {
  return (
    <colgroup>
      {widths.map((width, index) => (
        <col
          // Columns are positional and a table's column set never reorders, so
          // the index is the only stable identity available here.
          key={index}
          style={typeof width === "number" ? { width: `${width}px` } : undefined}
        />
      ))}
    </colgroup>
  );
}

export function SortHead<Column extends string>({
  label,
  column,
  sort,
  direction,
  setSort,
  className,
  sticky,
  resizeHandle
}: {
  label: string;
  column: Column;
  sort: Column;
  direction: SortDirection;
  setSort: (column: Column) => void;
  className?: string;
  sticky?: "left" | "right";
  resizeHandle?: ReactNode;
}) {
  const active = sort === column;
  const Icon = active ? ArrowDownIcon : CaretUpDownIcon;
  return (
    <Table.Head
      className={className}
      sticky={sticky}
      aria-label={label}
      aria-sort={active ? (direction === "asc" ? "ascending" : "descending") : "none"}
    >
      <button
        type="button"
        className="bf-sort-button inline-flex min-w-0 items-center gap-1.5 text-left font-medium text-inherit hover:text-kumo-default"
        onClick={() => setSort(column)}
      >
        <span className="min-w-0 truncate">{label}</span>
        <Icon
          weight="bold"
          aria-hidden="true"
          data-sort-indicator={active ? direction : "none"}
          className={`bf-sort-icon pointer-events-none size-3 shrink-0 opacity-50 ${active ? "transition-transform duration-200 ease-in-out" : ""} ${active && direction === "asc" ? "rotate-180" : ""}`}
        />
      </button>
      {resizeHandle}
    </Table.Head>
  );
}

export function TableEmpty({
  children,
  colSpan,
  description
}: {
  children: string;
  colSpan: number;
  description?: string;
}) {
  return (
    <Table.Row>
      <Table.Cell colSpan={colSpan}>
        <Empty size="sm" title={children} description={description} className="min-h-32 justify-center" />
      </Table.Cell>
    </Table.Row>
  );
}

/** Error row for failed table queries — visually distinct from the empty state. */
export function TableError({ children, colSpan }: { children: string; colSpan: number }) {
  return (
    <Table.Row>
      <Table.Cell colSpan={colSpan}>
        <div className="flex min-h-32 items-center justify-center gap-2 text-sm text-kumo-danger">
          <WarningCircleIcon className="size-4 shrink-0" aria-hidden="true" />
          {children}
        </div>
      </Table.Cell>
    </Table.Row>
  );
}

export function TableLoading({ colSpan }: { colSpan: number }) {
  return (
    <Table.Row>
      <Table.Cell colSpan={colSpan}>
        <div className="flex min-h-32 items-center justify-center"><Loader size={20} /></div>
      </Table.Cell>
    </Table.Row>
  );
}

export function AdminPagination({
  page,
  setPage,
  perPage,
  setPerPage,
  total,
  pageSizes = [10, 25, 50, 100]
}: {
  page: number;
  setPage: (page: number) => void;
  perPage: number;
  setPerPage: (size: number) => void;
  total: number;
  pageSizes?: number[];
}) {
  if (total <= 0) {
    return <div className="mt-1 text-sm text-kumo-subtle">0 items</div>;
  }
  return (
    <Pagination page={page} setPage={setPage} perPage={perPage} totalCount={total} className="mt-1">
      <Pagination.Info>
        {({ pageShowingRange, totalCount }) => (
          <span><strong>{pageShowingRange}</strong> of {totalCount} items</span>
        )}
      </Pagination.Info>
      <Pagination.Separator />
      <Pagination.PageSize value={perPage} onChange={setPerPage} options={pageSizes} label="Items per page:" />
      <Pagination.Controls controls="simple" />
    </Pagination>
  );
}
