import { SortAscendingIcon, SortDescendingIcon } from "@phosphor-icons/react";
import { Loader, Pagination, Table } from "@cloudflare/kumo";

export type SortDirection = "asc" | "desc";

export function SortHead<Column extends string>({
  label,
  column,
  sort,
  direction,
  setSort,
  className
}: {
  label: string;
  column: Column;
  sort: Column;
  direction: SortDirection;
  setSort: (column: Column) => void;
  className?: string;
}) {
  const active = sort === column;
  const Icon = active && direction === "desc" ? SortDescendingIcon : SortAscendingIcon;
  return (
    <Table.Head className={className}>
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

export function TableEmpty({ children, colSpan }: { children: string; colSpan: number }) {
  return (
    <Table.Row>
      <Table.Cell colSpan={colSpan}>
        <div className="flex min-h-32 items-center justify-center text-sm text-kumo-subtle">{children}</div>
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
  return (
    <Pagination page={page} setPage={setPage} perPage={perPage} totalCount={total} className="mt-1">
      {total > 0 ? (
        <Pagination.Info>
          {({ pageShowingRange, totalCount }) => (
            <span><strong>{pageShowingRange}</strong> of {totalCount} items</span>
          )}
        </Pagination.Info>
      ) : <span className="text-sm text-kumo-subtle">0 items</span>}
      <Pagination.Separator />
      <Pagination.PageSize value={perPage} onChange={setPerPage} options={pageSizes} label="Items per page:" />
      {total > 0 ? <Pagination.Controls controls="simple" /> : null}
    </Pagination>
  );
}
