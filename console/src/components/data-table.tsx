import type { ReactNode } from "react";
import { useNavigate } from "react-router";

import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";

import { Empty, ErrorState } from "./page";

export type Column<T> = {
  header: ReactNode;
  cell: (row: T) => ReactNode;
  className?: string;
};

export function DataTable<T>({
  columns,
  rows,
  loading,
  error,
  empty = "Nothing here.",
  rowKey,
  rowHref,
  rowClassName,
}: {
  columns: Column<T>[];
  rows: T[] | undefined;
  loading?: boolean;
  error?: unknown;
  empty?: ReactNode;
  rowKey: (row: T) => string | number;
  rowHref?: (row: T) => string;
  rowClassName?: (row: T) => string | undefined;
}) {
  const navigate = useNavigate();
  if (error) return <ErrorState error={error} />;
  return (
    <Table>
      <TableHeader>
        <TableRow>
          {columns.map((c, i) => (
            <TableHead key={i} className={c.className}>
              {c.header}
            </TableHead>
          ))}
        </TableRow>
      </TableHeader>
      <TableBody>
        {loading &&
          Array.from({ length: 4 }).map((_, i) => (
            <TableRow key={i}>
              {columns.map((_, j) => (
                <TableCell key={j}>
                  <Skeleton className="h-4 w-full" />
                </TableCell>
              ))}
            </TableRow>
          ))}
        {!loading && rows?.length === 0 && (
          <TableRow>
            <TableCell colSpan={columns.length}>
              <Empty>{empty}</Empty>
            </TableCell>
          </TableRow>
        )}
        {!loading &&
          rows?.map((row) => (
            <TableRow
              key={rowKey(row)}
              className={cn(rowHref && "cursor-pointer", rowClassName?.(row))}
              onClick={(e) => {
                if (!rowHref) return;
                // Let buttons and links inside the row do their own thing.
                if ((e.target as HTMLElement).closest("button,a,input,label")) return;
                navigate(rowHref(row));
              }}
            >
              {columns.map((c, i) => (
                <TableCell key={i} className={c.className}>
                  {c.cell(row)}
                </TableCell>
              ))}
            </TableRow>
          ))}
      </TableBody>
    </Table>
  );
}
