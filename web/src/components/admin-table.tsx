import type { ReactNode } from "react";
import { SortAscendingIcon, SortDescendingIcon, WarningCircleIcon } from "@phosphor-icons/react";
import { Empty, Loader, Pagination, Table } from "@cloudflare/kumo";

export type SortDirection = "asc" | "desc";

/**
 * Canonical chrome for admin data tables: rounded card with a hairline border
 * and a thin-scrollbar horizontal scroll area. Pages render `<Table>` inside.
 */
export function TableCard({ children, className = "" }: { children: ReactNode; className?: string }) {
  return (
    <div className={`overflow-hidden rounded-lg border border-kumo-line bg-kumo-base ${className}`}>
      <div className="bf-table-scroll overflow-x-auto overscroll-x-contain">{children}</div>
    </div>
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
