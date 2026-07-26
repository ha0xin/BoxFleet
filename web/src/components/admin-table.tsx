import type { ReactNode } from "react";
import { SortAscendingIcon, SortDescendingIcon, WarningCircleIcon } from "@phosphor-icons/react";
import { Empty, Loader, Pagination, Table } from "@cloudflare/kumo";

export type SortDirection = "asc" | "desc";

/**
 * Canonical chrome for admin data tables: rounded card with a hairline border
 * and a thin-scrollbar horizontal scroll area. Pages render `<Table>` inside.
 *
 * The card is only half of the horizontal-overflow contract. Every admin page
 * root is a grid item, and a grid item's default `min-width: auto` means it can
 * never be narrower than its min-content size — so a wide table's minimum width
 * leaks straight past this scroll container and widens the whole page instead.
 * Page roots must therefore carry `min-w-0`; see `docs/web-ui.md`.
 */
export function TableCard({ children, className = "" }: { children: ReactNode; className?: string }) {
  return (
    <div className={`overflow-hidden rounded-lg border border-kumo-line bg-kumo-base ${className}`}>
      <div className="bf-table-scroll overflow-x-auto overscroll-x-contain">{children}</div>
    </div>
  );
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
 * substrate Kumo's `Table.ResizeHandle` needs, so a future draggable-resize pass
 * only has to feed these widths from TanStack's `column.getSize()`.
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
  sticky
}: {
  label: string;
  column: Column;
  sort: Column;
  direction: SortDirection;
  setSort: (column: Column) => void;
  className?: string;
  sticky?: "left" | "right";
}) {
  const active = sort === column;
  const Icon = active && direction === "desc" ? SortDescendingIcon : SortAscendingIcon;
  return (
    <Table.Head
      className={className}
      sticky={sticky}
      aria-sort={active ? (direction === "asc" ? "ascending" : "descending") : "none"}
    >
      <button
        type="button"
        className="inline-flex items-center gap-1 whitespace-nowrap text-left font-medium text-kumo-default hover:text-kumo-strong"
        onClick={() => setSort(column)}
      >
        {label}
        <Icon className={`size-3.5 ${active ? "text-kumo-default" : "text-kumo-subtle"}`} />
      </button>
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
